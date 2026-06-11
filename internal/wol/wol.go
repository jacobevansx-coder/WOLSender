package wol

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	DefaultPort     = 9
	MagicPacketLen = 102
	autoInterfaceID = "auto"
)

type InterfaceAddress struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Index           int    `json:"index"`
	HardwareAddress string `json:"hardwareAddress,omitempty"`
	IPv4            string `json:"ipv4"`
	PrefixLength    int    `json:"prefixLength"`
	Broadcast       string `json:"broadcast"`
}

type WakeResult struct {
	MAC           string `json:"mac"`
	InterfaceID   string `json:"interfaceId"`
	InterfaceName string `json:"interfaceName"`
	LocalAddress  string `json:"localAddress"`
	Broadcast     string `json:"broadcast"`
	Port          int    `json:"port"`
	BytesSent     int    `json:"bytesSent"`
}

func ListInterfaces() ([]InterfaceAddress, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	results := make([]InterfaceAddress, 0)
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ipv4 := ipNet.IP.To4()
			if ipv4 == nil {
				continue
			}

			broadcast, prefixLength, ok := IPv4Broadcast(ipv4, ipNet.Mask)
			if !ok {
				continue
			}

			results = append(results, InterfaceAddress{
				ID:              interfaceID(iface.Index, ipv4),
				Name:            iface.Name,
				Index:           iface.Index,
				HardwareAddress: strings.ToUpper(iface.HardwareAddr.String()),
				IPv4:            ipv4.String(),
				PrefixLength:    prefixLength,
				Broadcast:       broadcast.String(),
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Index == results[j].Index {
			return results[i].IPv4 < results[j].IPv4
		}
		return results[i].Index < results[j].Index
	})

	return results, nil
}

func NormalizeMAC(input string) (net.HardwareAddr, string, error) {
	var compact strings.Builder
	for _, r := range strings.TrimSpace(input) {
		switch {
		case r == ':' || r == '-' || r == '.':
			continue
		case unicode.IsSpace(r):
			continue
		case isHexRune(r):
			compact.WriteRune(r)
		default:
			return nil, "", fmt.Errorf("MAC address contains an invalid character: %q", r)
		}
	}

	value := compact.String()
	if len(value) != 12 {
		return nil, "", fmt.Errorf("MAC address must contain exactly 12 hexadecimal digits")
	}

	bytes, err := hex.DecodeString(value)
	if err != nil {
		return nil, "", fmt.Errorf("MAC address is not valid hexadecimal")
	}

	hw := net.HardwareAddr(bytes)
	return hw, strings.ToUpper(hw.String()), nil
}

func BuildMagicPacket(hw net.HardwareAddr) ([]byte, error) {
	if len(hw) != 6 {
		return nil, fmt.Errorf("MAC address must be 6 bytes")
	}

	packet := make([]byte, MagicPacketLen)
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}

	offset := 6
	for i := 0; i < 16; i++ {
		copy(packet[offset:offset+6], hw)
		offset += 6
	}

	return packet, nil
}

func Wake(macInput, selectedInterfaceID string, port int) (WakeResult, error) {
	if port == 0 {
		port = DefaultPort
	}
	if port < 1 || port > 65535 {
		return WakeResult{}, fmt.Errorf("port must be between 1 and 65535")
	}

	hw, normalizedMAC, err := NormalizeMAC(macInput)
	if err != nil {
		return WakeResult{}, err
	}

	packet, err := BuildMagicPacket(hw)
	if err != nil {
		return WakeResult{}, err
	}

	interfaceID := strings.TrimSpace(selectedInterfaceID)
	if interfaceID == "" {
		interfaceID = autoInterfaceID
	}

	result := WakeResult{
		MAC:           normalizedMAC,
		InterfaceID:   interfaceID,
		InterfaceName: "Automatic route",
		Broadcast:     "255.255.255.255",
		Port:          port,
	}

	localAddr := &net.UDPAddr{Port: 0}
	remoteAddr := &net.UDPAddr{IP: net.IPv4(255, 255, 255, 255), Port: port}

	if interfaceID != autoInterfaceID {
		selected, err := findInterfaceAddress(interfaceID)
		if err != nil {
			return WakeResult{}, err
		}

		localIP := net.ParseIP(selected.IPv4).To4()
		broadcastIP := net.ParseIP(selected.Broadcast).To4()
		if localIP == nil || broadcastIP == nil {
			return WakeResult{}, fmt.Errorf("selected interface has invalid IPv4 data")
		}

		localAddr.IP = localIP
		remoteAddr.IP = broadcastIP
		result.InterfaceName = selected.Name
		result.LocalAddress = selected.IPv4
		result.Broadcast = selected.Broadcast
	}

	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return WakeResult{}, fmt.Errorf("open UDP socket: %w", err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return WakeResult{}, fmt.Errorf("set UDP write deadline: %w", err)
	}

	bytesSent, err := conn.WriteToUDP(packet, remoteAddr)
	if err != nil {
		return WakeResult{}, fmt.Errorf("send magic packet: %w", err)
	}
	if bytesSent != len(packet) {
		return WakeResult{}, fmt.Errorf("sent %d of %d packet bytes", bytesSent, len(packet))
	}

	if result.LocalAddress == "" {
		result.LocalAddress = conn.LocalAddr().String()
	}
	result.BytesSent = bytesSent
	return result, nil
}

func IPv4Broadcast(ip net.IP, mask net.IPMask) (net.IP, int, bool) {
	ipv4 := ip.To4()
	if ipv4 == nil || len(mask) != net.IPv4len {
		return nil, 0, false
	}

	prefixLength, bits := mask.Size()
	if bits != 32 {
		return nil, 0, false
	}

	broadcast := make(net.IP, net.IPv4len)
	for i := 0; i < net.IPv4len; i++ {
		broadcast[i] = ipv4[i] | ^mask[i]
	}

	return broadcast, prefixLength, true
}

func findInterfaceAddress(id string) (InterfaceAddress, error) {
	interfaces, err := ListInterfaces()
	if err != nil {
		return InterfaceAddress{}, err
	}

	for _, iface := range interfaces {
		if iface.ID == id {
			return iface, nil
		}
	}

	return InterfaceAddress{}, errors.New("selected network interface is no longer available")
}

func interfaceID(index int, ip net.IP) string {
	return fmt.Sprintf("%d|%s", index, ip.String())
}

func isHexRune(r rune) bool {
	return r >= '0' && r <= '9' ||
		r >= 'a' && r <= 'f' ||
		r >= 'A' && r <= 'F'
}
