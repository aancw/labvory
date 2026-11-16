package integrations

import (
	"net/netip"
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

// TestParseArpAnMacOSRealOutput covers a real macOS `arp -a` dump: compressed
// MAC groups (dropped leading zeros), permanent entries and the broadcast row.
// Broadcast/multicast filtering happens later, in the sync loop.
func TestParseArpAnMacOSRealOutput(t *testing.T) {
	content := `? (192.168.1.1) at ec:e6:a2:17:9c:e8 on en0 ifscope [ethernet]
? (192.168.1.3) at 4e:97:cb:3c:52:c1 on en0 ifscope [ethernet]
? (192.168.1.4) at 80:9d:65:8b:be:0 on en0 ifscope [ethernet]
? (192.168.1.5) at 7e:1e:b9:ed:d6:f on en0 ifscope [ethernet]
? (192.168.1.9) at f8:e4:3b:31:5a:ec on en0 ifscope [ethernet]
? (192.168.1.10) at 1a:f0:ec:5a:ae:d4 on en0 ifscope [ethernet]
? (192.168.1.11) at 2:0:a5:e9:e7:1d on en0 ifscope [ethernet]
? (192.168.1.19) at d0:11:e5:ed:ed:dd on en0 ifscope permanent [ethernet]
? (192.168.1.111) at bc:24:11:64:e3:40 on en0 ifscope [ethernet]
? (192.168.1.119) at bc:24:11:a2:25:69 on en0 ifscope [ethernet]
? (192.168.1.255) at ff:ff:ff:ff:ff:ff on en0
`
	entries := parseArpAnOutput(content)
	if len(entries) != 11 {
		t.Fatalf("expected 11 entries, got %d: %+v", len(entries), entries)
	}
	byIP := map[string]string{}
	for _, e := range entries {
		byIP[e.IP.String()] = e.MAC
	}
	// Compressed groups must be zero-padded.
	want := map[string]string{
		"192.168.1.4":   "80:9d:65:8b:be:00",
		"192.168.1.5":   "7e:1e:b9:ed:d6:0f",
		"192.168.1.11":  "02:00:a5:e9:e7:1d",
		"192.168.1.19":  "d0:11:e5:ed:ed:dd", // permanent entries are kept
		"192.168.1.255": "ff:ff:ff:ff:ff:ff", // broadcast row parses; sync filters it
	}
	for ip, mac := range want {
		if byIP[ip] != mac {
			t.Errorf("%s = %q, want %q", ip, byIP[ip], mac)
		}
	}
}

// TestIsUnicastMAC covers the device-vs-link-layer-group filter.
func TestIsUnicastMAC(t *testing.T) {
	cases := map[string]bool{
		"aa:bb:cc:dd:ee:ff": true,
		"7e:1e:b9:ed:d6:0f": true, // locally administered, unicast
		"02:00:a5:e9:e7:1d": true,
		"bc:24:11:64:e3:40": true,
		"ff:ff:ff:ff:ff:ff": false, // broadcast
		"01:00:5e:00:00:01": false, // IPv4 multicast (mDNS)
		"33:33:00:00:00:01": false, // IPv6 multicast
	}
	for mac, want := range cases {
		if got := isUnicastMAC(mac); got != want {
			t.Errorf("isUnicastMAC(%s) = %v, want %v", mac, got, want)
		}
	}
}

// TestNormalizeMAC covers the accepted MAC spellings.
func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"AA:BB:CC:DD:EE:FF":   "aa:bb:cc:dd:ee:ff",
		"aa-bb-cc-dd-ee-ff":   "aa:bb:cc:dd:ee:ff",
		"aabbccddeeff":        "aa:bb:cc:dd:ee:ff",
		" AA-BB-CC-DD-EE-FF ": "aa:bb:cc:dd:ee:ff",
		"aabb.ccdd.eeff":      "", // Cisco dot form is not supported
		"00:11:32:AA:ZZ:CC":   "", // invalid hex
		"aa:bb:cc:dd:ee":      "", // too short
		"incomplete":          "",
		"00:00:00:00:00:00":   "00:00:00:00:00:00", // valid shape; callers skip zero MACs themselves
	}
	for in, want := range cases {
		if got := normalizeMAC(in); got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestArpGroupByMac covers the one-device-per-MAC grouping (last sighting wins).
func TestArpGroupByMac(t *testing.T) {
	entries := []arpEntry{
		{IP: netip.MustParseAddr("192.168.1.13"), MAC: "aa:bb:cc:00:00:01"},
		{IP: netip.MustParseAddr("192.168.1.13"), MAC: "aa:bb:cc:00:00:01"}, // dup sighting
		{IP: netip.MustParseAddr("192.168.1.20"), MAC: "aa:bb:cc:00:00:01"}, // later IP wins
		{IP: netip.MustParseAddr("192.168.139.7"), MAC: "aa:bb:cc:00:00:02"},
		{IP: netip.MustParseAddr("192.168.1.30"), MAC: ""}, // invalid: dropped
	}
	got := arpGroupByMac(entries)
	if len(got) != 2 {
		t.Fatalf("expected 2 MACs, got %d: %v", len(got), got)
	}
	if got["aa:bb:cc:00:00:01"] != "192.168.1.20" {
		t.Errorf("mac 1 = %q, want last sighting 192.168.1.20", got["aa:bb:cc:00:00:01"])
	}
	if got["aa:bb:cc:00:00:02"] != "192.168.139.7" {
		t.Errorf("mac 2 = %q", got["aa:bb:cc:00:00:02"])
	}
}
