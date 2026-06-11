package wol

import (
	"bytes"
	"net"
	"testing"
)

func TestNormalizeMACAcceptsCommonFormats(t *testing.T) {
	tests := map[string]string{
		"aa:bb:cc:dd:ee:ff": "AA:BB:CC:DD:EE:FF",
		"AA-BB-CC-DD-EE-FF": "AA:BB:CC:DD:EE:FF",
		"aabb.ccdd.eeff":    "AA:BB:CC:DD:EE:FF",
		"aabbccddeeff":      "AA:BB:CC:DD:EE:FF",
	}

	for input, want := range tests {
		_, got, err := NormalizeMAC(input)
		if err != nil {
			t.Fatalf("NormalizeMAC(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeMAC(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeMACRejectsInvalidInput(t *testing.T) {
	tests := []string{
		"",
		"aa:bb:cc:dd:ee",
		"aa:bb:cc:dd:ee:ff:00",
		"aa:bb:cc:dd:ee:gg",
	}

	for _, input := range tests {
		if _, _, err := NormalizeMAC(input); err == nil {
			t.Fatalf("NormalizeMAC(%q) returned nil error", input)
		}
	}
}

func TestBuildMagicPacket(t *testing.T) {
	hw, _, err := NormalizeMAC("01:23:45:67:89:ab")
	if err != nil {
		t.Fatal(err)
	}

	packet, err := BuildMagicPacket(hw)
	if err != nil {
		t.Fatal(err)
	}

	if len(packet) != MagicPacketLen {
		t.Fatalf("packet length = %d, want %d", len(packet), MagicPacketLen)
	}
	if !bytes.Equal(packet[:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("packet prefix is not six 0xff bytes")
	}

	for offset := 6; offset < len(packet); offset += 6 {
		if !bytes.Equal(packet[offset:offset+6], hw) {
			t.Fatalf("packet MAC copy at offset %d does not match", offset)
		}
	}
}

func TestIPv4Broadcast(t *testing.T) {
	got, prefixLength, ok := IPv4Broadcast(net.ParseIP("192.168.10.24"), net.CIDRMask(24, 32))
	if !ok {
		t.Fatal("IPv4Broadcast returned false")
	}
	if prefixLength != 24 {
		t.Fatalf("prefix length = %d, want 24", prefixLength)
	}
	if got.String() != "192.168.10.255" {
		t.Fatalf("broadcast = %s, want 192.168.10.255", got)
	}
}
