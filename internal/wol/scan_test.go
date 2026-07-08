package wol

import (
	"net"
	"strings"
	"testing"
)

func TestHostsGeneratesUsableRange(t *testing.T) {
	hosts, err := Hosts(net.ParseIP("192.168.10.5"), 24)
	if err != nil {
		t.Fatalf("Hosts returned error: %v", err)
	}
	if len(hosts) != 254 {
		t.Fatalf("host count = %d, want 254", len(hosts))
	}
	if hosts[0] != "192.168.10.1" {
		t.Fatalf("first host = %s, want 192.168.10.1", hosts[0])
	}
	if hosts[len(hosts)-1] != "192.168.10.254" {
		t.Fatalf("last host = %s, want 192.168.10.254", hosts[len(hosts)-1])
	}
	for _, h := range hosts {
		if h == "192.168.10.0" || h == "192.168.10.255" {
			t.Fatalf("host list must exclude network/broadcast, found %s", h)
		}
	}
}

func TestHostsSmallSubnet(t *testing.T) {
	hosts, err := Hosts(net.ParseIP("192.168.1.1"), 30)
	if err != nil {
		t.Fatalf("Hosts returned error: %v", err)
	}
	want := []string{"192.168.1.1", "192.168.1.2"}
	if len(hosts) != len(want) {
		t.Fatalf("host count = %d, want %d", len(hosts), len(want))
	}
	for i, h := range hosts {
		if h != want[i] {
			t.Fatalf("host[%d] = %s, want %s", i, h, want[i])
		}
	}
}

func TestHostsRefusesLargeSubnet(t *testing.T) {
	_, err := Hosts(net.ParseIP("10.0.0.1"), 16)
	if err == nil {
		t.Fatal("Hosts(/16) should exceed the scan cap and return an error")
	}
	// The hint must name a real prefix, not the /33 an unsigned underflow produced.
	if !strings.Contains(err.Error(), "/23") {
		t.Fatalf("error should hint scanning a /23, got: %v", err)
	}
}

func TestPrefixForLimit(t *testing.T) {
	// A /23 has 510 usable hosts (<= 512 cap); a /22 has 1022 (refused).
	if got := prefixForLimit(); got != 23 {
		t.Fatalf("prefixForLimit() = %d, want 23", got)
	}
}

func TestHostsRejectsDegeneratePrefixes(t *testing.T) {
	for _, prefix := range []int{31, 32} {
		if _, err := Hosts(net.ParseIP("192.168.1.1"), prefix); err == nil {
			t.Fatalf("Hosts(/%d) should have no scannable hosts", prefix)
		}
	}
}

func TestParseARPWindows(t *testing.T) {
	output := `
Interface: 192.168.1.10 --- 0x5
  Internet Address      Physical Address      Type
  192.168.1.1           aa-bb-cc-dd-ee-ff     dynamic
  192.168.1.42          00-11-22-33-44-55     dynamic
  192.168.1.255         ff-ff-ff-ff-ff-ff     static
  224.0.0.22            01-00-5e-00-00-16     static
`
	got := parseARP(output)
	if got["192.168.1.1"] != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("192.168.1.1 = %q, want AA:BB:CC:DD:EE:FF", got["192.168.1.1"])
	}
	if got["192.168.1.42"] != "00:11:22:33:44:55" {
		t.Fatalf("192.168.1.42 = %q, want 00:11:22:33:44:55", got["192.168.1.42"])
	}
	if _, ok := got["192.168.1.255"]; ok {
		t.Fatal("broadcast MAC must be skipped")
	}
	if _, ok := got["224.0.0.22"]; ok {
		t.Fatal("multicast MAC must be skipped")
	}
}

func TestParseARPMacOS(t *testing.T) {
	output := `? (192.168.1.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
? (192.168.1.7) at (incomplete) on en0 [ethernet]
? (192.168.1.9) at 0:11:22:33:44:55 on en0 ifscope [ethernet]
? (192.168.1.20) at 3c:37:86:5d:4a:1 on en0 ifscope [ethernet]`
	got := parseARP(output)
	if got["192.168.1.1"] != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("192.168.1.1 = %q, want AA:BB:CC:DD:EE:FF", got["192.168.1.1"])
	}
	if _, ok := got["192.168.1.7"]; ok {
		t.Fatal("incomplete entry must be skipped")
	}
	// macOS/BSD collapses leading zeros; those octets must be re-padded, not dropped.
	if got["192.168.1.9"] != "00:11:22:33:44:55" {
		t.Fatalf("192.168.1.9 = %q, want 00:11:22:33:44:55", got["192.168.1.9"])
	}
	if got["192.168.1.20"] != "3C:37:86:5D:4A:01" {
		t.Fatalf("192.168.1.20 = %q, want 3C:37:86:5D:4A:01", got["192.168.1.20"])
	}
}

func TestParseProcNetARP(t *testing.T) {
	content := `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         aa:bb:cc:dd:ee:ff     *        eth0
192.168.1.50     0x1         0x0         00:00:00:00:00:00     *        eth0
192.168.1.51     0x1         0x2         00:11:22:33:44:55     *        eth0`
	got := parseProcNetARP(content)
	if got["192.168.1.1"] != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("192.168.1.1 = %q, want AA:BB:CC:DD:EE:FF", got["192.168.1.1"])
	}
	if got["192.168.1.51"] != "00:11:22:33:44:55" {
		t.Fatalf("192.168.1.51 = %q, want 00:11:22:33:44:55", got["192.168.1.51"])
	}
	if _, ok := got["192.168.1.50"]; ok {
		t.Fatal("incomplete (0x0) entry must be skipped")
	}
}

func TestIPLess(t *testing.T) {
	if !ipLess("192.168.1.9", "192.168.1.10") {
		t.Fatal("192.168.1.9 should sort before 192.168.1.10 numerically")
	}
	if ipLess("192.168.1.10", "192.168.1.9") {
		t.Fatal("192.168.1.10 should not sort before 192.168.1.9")
	}
}
