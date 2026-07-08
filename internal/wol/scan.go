package wol

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxScanHosts caps how many addresses a single /api/scan will probe. A /24
// (254 hosts) and a /23 (510) pass; anything larger is refused with a clear
// error so a mistyped /16 cannot fan out to 65k pings.
// ponytail: fixed cap; make it a request field only if someone needs bigger.
const maxScanHosts = 512

const (
	defaultPingTimeout = 700 * time.Millisecond
	defaultConcurrency = 64
	reverseDNSTimeout  = 800 * time.Millisecond
)

// Device is one host observed on the LAN during a scan. MAC is empty when the
// neighbor/ARP table did not reveal it, in which case the host cannot be woken
// until a MAC is supplied and remembered by the client.
type Device struct {
	IP            string `json:"ip"`
	Hostname      string `json:"hostname"`
	MAC           string `json:"mac"`
	InterfaceID   string `json:"interfaceId"`
	InterfaceName string `json:"interfaceName"`
	Reachable     bool   `json:"reachable"`
	Source        string `json:"source"`
	LastSeen      string `json:"lastSeen"`
}

// ScanResult is the payload returned to the UI for one subnet scan.
type ScanResult struct {
	InterfaceID   string   `json:"interfaceId"`
	InterfaceName string   `json:"interfaceName"`
	HostsScanned  int      `json:"hostsScanned"`
	Reachable     int      `json:"reachable"`
	Devices       []Device `json:"devices"`
}

// ScanOptions tunes a scan; the zero value uses conservative defaults.
type ScanOptions struct {
	PingTimeout time.Duration
	Concurrency int
}

// Scan probes every usable host on the selected interface's IPv4 subnet using
// the OS ping command with bounded concurrency, then reads the neighbor/ARP
// table (which the pings populate) to attach MAC addresses and reverse DNS.
func Scan(ctx context.Context, selectedInterfaceID string, opts ScanOptions) (ScanResult, error) {
	id := strings.TrimSpace(selectedInterfaceID)
	if id == "" || id == autoInterfaceID {
		return ScanResult{}, fmt.Errorf("choose a specific interface to scan the LAN")
	}

	iface, err := findInterfaceAddress(id)
	if err != nil {
		return ScanResult{}, err
	}

	localIP := net.ParseIP(iface.IPv4)
	hosts, err := Hosts(localIP, iface.PrefixLength)
	if err != nil {
		return ScanResult{}, err
	}

	if opts.PingTimeout <= 0 {
		opts.PingTimeout = defaultPingTimeout
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = defaultConcurrency
	}

	self := localIP.To4().String()
	reachable := pingHosts(ctx, hosts, self, opts)
	if ctx.Err() != nil {
		return ScanResult{}, ctx.Err()
	}

	// Pings just populated the neighbor cache; read it once for all hosts.
	neighbors := neighborTable(ctx)

	now := time.Now().UTC().Format(time.RFC3339)
	devices := make([]Device, len(reachable))
	for i, ip := range reachable {
		device := Device{
			IP:            ip,
			InterfaceID:   iface.ID,
			InterfaceName: iface.Name,
			Reachable:     true,
			Source:        "ping",
			LastSeen:      now,
		}
		if mac, ok := neighbors[ip]; ok {
			device.MAC = mac
			device.Source = "ping+arp"
		}
		devices[i] = device
	}
	resolveHostnames(ctx, devices, opts.Concurrency)

	sort.Slice(devices, func(i, j int) bool {
		return ipLess(devices[i].IP, devices[j].IP)
	})

	return ScanResult{
		InterfaceID:   iface.ID,
		InterfaceName: iface.Name,
		HostsScanned:  len(hosts) - 1, // self is always in the list but never pinged
		Reachable:     len(devices),
		Devices:       devices,
	}, nil
}

// Hosts returns every usable host address in the IPv4 subnet containing ip with
// the given prefix length (network and broadcast excluded). It refuses subnets
// with more than maxScanHosts usable addresses.
func Hosts(ip net.IP, prefixLength int) ([]string, error) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("interface address is not IPv4")
	}
	if prefixLength < 0 || prefixLength > 32 {
		return nil, fmt.Errorf("invalid prefix length /%d", prefixLength)
	}

	hostBits := 32 - prefixLength
	if hostBits < 2 {
		return nil, fmt.Errorf("subnet /%d has no scannable hosts", prefixLength)
	}

	count := (uint64(1) << hostBits) - 2 // drop network + broadcast
	if count > maxScanHosts {
		return nil, fmt.Errorf("subnet /%d has %d hosts, which exceeds the scan limit of %d; scan a /%d or smaller",
			prefixLength, count, maxScanHosts, prefixForLimit())
	}

	network := binary.BigEndian.Uint32(ipv4.Mask(net.CIDRMask(prefixLength, 32)))
	hosts := make([]string, 0, count)
	for i := uint64(1); i <= count; i++ {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], network+uint32(i))
		hosts = append(hosts, net.IP(b[:]).String())
	}
	return hosts, nil
}

// prefixForLimit reports the smallest prefix (largest subnet) that still fits
// under maxScanHosts, for use in the "scan a /N or smaller" hint. Iterating on
// host bits (>= 2) avoids the unsigned underflow that a /32 (0 host bits) would
// cause in the (1<<bits)-2 host-count formula.
func prefixForLimit() int {
	for hostBits := 2; hostBits <= 32; hostBits++ {
		if (uint64(1)<<hostBits)-2 > maxScanHosts {
			return 32 - (hostBits - 1)
		}
	}
	return 0
}

func pingHosts(ctx context.Context, hosts []string, self string, opts ScanOptions) []string {
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		reachable []string
		sem       = make(chan struct{}, opts.Concurrency)
	)

	for _, host := range hosts {
		if host == self {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			if pingReachable(ctx, ip, opts.PingTimeout) {
				mu.Lock()
				reachable = append(reachable, ip)
				mu.Unlock()
			}
		}(host)
	}

	wg.Wait()
	return reachable
}

// pingReachable sends one echo request via the OS ping command. Using the OS
// command avoids the raw-socket privileges an in-process ICMP would require.
// ponytail: shells out per host; a raw-socket pinger would be faster but needs
// admin/CAP_NET_RAW — not worth it for a LAN sweep.
func pingReachable(ctx context.Context, ip string, timeout time.Duration) bool {
	out, err := pingCommand(ctx, ip, timeout).CombinedOutput()
	if err != nil {
		return false
	}
	// Windows ping exits 0 even for "Destination host unreachable" replies from
	// a router, so confirm a real echo reply by its TTL field.
	if runtime.GOOS == "windows" {
		return strings.Contains(strings.ToUpper(string(out)), "TTL=")
	}
	return true
}

func pingCommand(ctx context.Context, ip string, timeout time.Duration) *exec.Cmd {
	millis := int(timeout / time.Millisecond)
	if millis < 1 {
		millis = 1
	}
	switch runtime.GOOS {
	case "windows":
		return exec.CommandContext(ctx, "ping", "-n", "1", "-w", strconv.Itoa(millis), ip)
	case "darwin":
		// macOS: -W is per-reply wait in milliseconds.
		return exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(millis), ip)
	default:
		// Linux/BSD iputils: -W is per-reply timeout in whole seconds.
		seconds := (millis + 999) / 1000
		if seconds < 1 {
			seconds = 1
		}
		return exec.CommandContext(ctx, "ping", "-c", "1", "-n", "-W", strconv.Itoa(seconds), ip)
	}
}

// resolveHostnames fills in each device's Hostname via reverse DNS, running the
// lookups concurrently so that hosts without a PTR record (each of which waits
// out the short timeout) do not stack up serially. Each goroutine writes a
// distinct slice index, so no locking is needed.
func resolveHostnames(ctx context.Context, devices []Device, concurrency int) {
	resolver := &net.Resolver{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := range devices {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			devices[i].Hostname = reverseDNS(ctx, resolver, devices[i].IP)
		}(i)
	}
	wg.Wait()
}

// reverseDNS returns the first PTR name for ip, or "" if none resolves within
// a short timeout.
func reverseDNS(ctx context.Context, resolver *net.Resolver, ip string) string {
	lookupCtx, cancel := context.WithTimeout(ctx, reverseDNSTimeout)
	defer cancel()
	names, err := resolver.LookupAddr(lookupCtx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// neighborTable maps IPv4 address -> canonical MAC from the OS neighbor cache.
// It is best-effort: an error reading the table yields an empty map so a scan
// still returns reachable hosts (just without MACs).
func neighborTable(ctx context.Context) map[string]string {
	if runtime.GOOS == "linux" {
		content, err := os.ReadFile("/proc/net/arp")
		if err != nil {
			return map[string]string{}
		}
		return parseProcNetARP(string(content))
	}

	out, err := exec.CommandContext(ctx, "arp", "-a").CombinedOutput()
	if err != nil {
		return map[string]string{}
	}
	return parseARP(string(out))
}

var (
	ipPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// Octets are {1,2} digits: macOS/BSD `arp -a` collapses leading zeros
	// (ether_ntoa uses %x), e.g. "3c:37:86:5d:4a:1" for ...:01.
	macPattern = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{1,2}[:-]){5}[0-9A-Fa-f]{1,2}\b`)
)

// canonicalizeARPMAC turns a MAC token from an OS neighbor table into the
// canonical AA:BB:CC:DD:EE:FF form, zero-padding single-digit octets so that
// macOS/BSD's collapsed output normalizes to the same 12-hex-digit value the
// wake path expects.
func canonicalizeARPMAC(token string) (string, bool) {
	parts := strings.FieldsFunc(token, func(r rune) bool { return r == ':' || r == '-' })
	if len(parts) != 6 {
		return "", false
	}
	var digits strings.Builder
	for _, part := range parts {
		if len(part) < 1 || len(part) > 2 {
			return "", false
		}
		if len(part) == 1 {
			digits.WriteByte('0')
		}
		digits.WriteString(part)
	}
	_, canonical, err := NormalizeMAC(digits.String())
	if err != nil {
		return "", false
	}
	return canonical, true
}

// parseARP reads `arp -a` output (Windows and macOS/BSD formats) into an
// IPv4 -> canonical MAC map, skipping broadcast/multicast/all-zero entries.
func parseARP(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		ip := ipPattern.FindString(line)
		mac := macPattern.FindString(line)
		if ip == "" || mac == "" {
			continue
		}
		if canonical, ok := canonicalizeARPMAC(mac); ok && !ignorableMAC(canonical) {
			result[ip] = canonical
		}
	}
	return result
}

// parseProcNetARP reads Linux /proc/net/arp into an IPv4 -> canonical MAC map,
// skipping incomplete (flag 0x0) entries.
func parseProcNetARP(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if i == 0 { // header row
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ip, flags, mac := fields[0], fields[2], fields[3]
		if flags == "0x0" {
			continue
		}
		if canonical, ok := canonicalizeARPMAC(mac); ok && !ignorableMAC(canonical) {
			result[ip] = canonical
		}
	}
	return result
}

func ignorableMAC(canonical string) bool {
	switch canonical {
	case "FF:FF:FF:FF:FF:FF", "00:00:00:00:00:00":
		return true
	}
	return strings.HasPrefix(canonical, "01:00:5E") // IPv4 multicast
}

func ipLess(a, b string) bool {
	ipA, ipB := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ipA == nil || ipB == nil {
		return a < b
	}
	return binary.BigEndian.Uint32(ipA) < binary.BigEndian.Uint32(ipB)
}
