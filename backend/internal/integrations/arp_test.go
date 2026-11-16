package integrations

import (
	"testing"
)

// TestParseProcArp covers /proc/net/arp rows including incomplete resolutions.
func TestParseProcArp(t *testing.T) {
	content := `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.11     0x1         0x2         aa:bb:cc:dd:ee:ff     *        eth0
192.168.1.99     0x1         0x0         00:00:00:00:00:00     *        eth0
192.168.1.25     0x1         0x2         00:11:32:AA:bb:CC     *        eth0
`
	entries := parseProcArp(content)
	if len(entries) != 2 {
		t.Fatalf("expected 2 complete entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].IP.String() != "192.168.1.11" || entries[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].MAC != "00:11:32:aa:bb:cc" { // normalized to lowercase
		t.Errorf("entry 1 = %+v", entries[1])
	}
}

// TestParseArpAnOutput covers `arp -an` (macOS and net-tools shapes) and
// incomplete entries.
func TestParseArpAnOutput(t *testing.T) {
	content := `? (192.168.1.11) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
? (192.168.1.99) at (incomplete) on en0 ifscope [ethernet]
? (192.168.1.25) at 00-11-32-aa-bb-cc on en0 ifscope [ethernet]
? (fe80::1%en0) at aa:bb:cc:dd:ee:1 on en0 ifscope permanent [ethernet]
? (192.168.1.30) at 0:1:2:3:4:5 on en0 ifscope [ethernet]
`
	entries := parseArpAnOutput(content)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("macOS line: %+v", entries[0])
	}
	if entries[1].MAC != "00:11:32:aa:bb:cc" { // dash form normalized
		t.Errorf("dash form: %+v", entries[1])
	}
	if entries[2].IP.String() != "fe80::1" { // zone stripped
		t.Errorf("ipv6 zone: %+v", entries[2])
	}
	if entries[3].MAC != "00:01:02:03:04:05" { // compressed form padded
		t.Errorf("compressed form: %+v", entries[3])
	}
}

// TestParseArpScanOutput covers arp-scan report lines vs. banner/summary lines.
func TestParseArpScanOutput(t *testing.T) {
	content := `Interface: eth0, type: EN10MB, MAC: de:ad:be:ef:00:01, IPv4: 192.168.1.5
Starting arp-scan 1.10.0 with 256 hosts (https://github.com/royhills/arp-scan)
192.168.1.11	aa:bb:cc:dd:ee:ff	Raspberry Pi Foundation
192.168.1.25	00:11:32:AA:BB:CC	Dell Inc.
192.168.1.1	f8:1a:67:ff:ff:ff	ASUSTek COMPUTER INC.
3 packets received by filter, 0 packets dropped by kernel
Ending arp-scan 1.10.0: 256 hosts scanned in 2.093 seconds (122.31 hosts/sec). 3 responded
`
	entries := parseArpScanOutput(content)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].IP.String() != "192.168.1.11" || entries[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].MAC != "00:11:32:aa:bb:cc" {
		t.Errorf("entry 1 = %+v", entries[1])
	}
}

// TestNormalizeMAC covers the accepted MAC spellings.
func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"AA:BB:CC:DD:EE:FF":  "aa:bb:cc:dd:ee:ff",
		"aa-bb-cc-dd-ee-ff":  "aa:bb:cc:dd:ee:ff",
		"aabbccddeeff":       "aa:bb:cc:dd:ee:ff",
		" AA-BB-CC-DD-EE-FF ": "aa:bb:cc:dd:ee:ff",
		"aabb.ccdd.eeff":     "", // Cisco dot form is not supported
		"00:11:32:AA:ZZ:CC":  "", // invalid hex
		"aa:bb:cc:dd:ee":     "", // too short
		"incomplete":         "",
		"00:00:00:00:00:00":  "00:00:00:00:00:00", // valid shape; callers skip zero MACs themselves
	}
	for in, want := range cases {
		if got := normalizeMAC(in); got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}
