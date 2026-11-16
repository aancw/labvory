package ipam

import (
	"math"
	"net/netip"
	"testing"
)

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := ParseCIDR(s)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", s, err)
	}
	return p
}

func TestParseCIDRNormalizes(t *testing.T) {
	p, err := ParseCIDR("10.0.0.77/24")
	if err != nil {
		t.Fatal(err)
	}
	if p.String() != "10.0.0.0/24" {
		t.Fatalf("expected masked network 10.0.0.0/24, got %s", p)
	}
	if _, err := ParseCIDR("not-a-cidr"); err == nil {
		t.Fatal("expected error for garbage input")
	}
	if _, err := ParseCIDR("10.0.0.1/33"); err == nil {
		t.Fatal("expected error for invalid prefix length")
	}
}

func TestUsableCount(t *testing.T) {
	cases := []struct {
		cidr string
		want uint64
	}{
		{"10.0.0.0/24", 254},
		{"10.0.0.0/30", 2},
		{"10.0.0.0/31", 2},  // RFC 3021 point-to-point
		{"10.0.0.0/32", 1},  // single host
		{"10.0.0.0/23", 510},
		{"10.0.0.0/1", math.MaxUint64 >> 0}, // saturates (2^31-2 < MaxUint64 actually)
		{"2001:db8::/64", math.MaxUint64},   // saturate: 2^64
		{"2001:db8::/126", 4},
	}
	for _, tc := range cases {
		got := UsableCount(mustPrefix(t, tc.cidr))
		if tc.cidr == "10.0.0.0/1" {
			if got != (uint64(1)<<31)-2 {
				t.Fatalf("%s: want %d, got %d", tc.cidr, (uint64(1)<<31)-2, got)
			}
			continue
		}
		if got != tc.want {
			t.Fatalf("%s: want %d, got %d", tc.cidr, tc.want, got)
		}
	}
}

func TestUsableRange(t *testing.T) {
	first, last := UsableRange(mustPrefix(t, "10.0.0.0/24"))
	if first.String() != "10.0.0.1" || last.String() != "10.0.0.254" {
		t.Fatalf("/24 range: got %s..%s", first, last)
	}

	first, last = UsableRange(mustPrefix(t, "10.0.0.0/31"))
	if first.String() != "10.0.0.0" || last.String() != "10.0.0.1" {
		t.Fatalf("/31 range: got %s..%s", first, last)
	}

	first, last = UsableRange(mustPrefix(t, "10.0.0.5/32"))
	if first.String() != "10.0.0.5" || last.String() != "10.0.0.5" {
		t.Fatalf("/32 range: got %s..%s", first, last)
	}

	first, last = UsableRange(mustPrefix(t, "10.0.0.0/30"))
	if first.String() != "10.0.0.1" || last.String() != "10.0.0.2" {
		t.Fatalf("/30 range: got %s..%s", first, last)
	}
}

func TestValidateHostAddress(t *testing.T) {
	p := mustPrefix(t, "10.0.0.0/24")

	if err := ValidateHostAddress(p, netip.MustParseAddr("10.0.0.1")); err != nil {
		t.Fatalf("first usable should pass: %v", err)
	}
	if err := ValidateHostAddress(p, netip.MustParseAddr("10.0.0.254")); err != nil {
		t.Fatalf("last usable should pass: %v", err)
	}
	if err := ValidateHostAddress(p, netip.MustParseAddr("10.0.0.0")); err == nil {
		t.Fatal("network address must be rejected")
	}
	if err := ValidateHostAddress(p, netip.MustParseAddr("10.0.0.255")); err == nil {
		t.Fatal("broadcast address must be rejected")
	}
	if err := ValidateHostAddress(p, netip.MustParseAddr("10.0.1.5")); err == nil {
		t.Fatal("address outside subnet must be rejected")
	}

	p31 := mustPrefix(t, "10.0.0.0/31")
	if err := ValidateHostAddress(p31, netip.MustParseAddr("10.0.0.0")); err != nil {
		t.Fatalf("/31 network address is usable: %v", err)
	}
	if err := ValidateHostAddress(p31, netip.MustParseAddr("10.0.0.1")); err != nil {
		t.Fatalf("/31 second address is usable: %v", err)
	}

	p32 := mustPrefix(t, "192.0.2.9/32")
	if err := ValidateHostAddress(p32, netip.MustParseAddr("192.0.2.9")); err != nil {
		t.Fatalf("/32 single host usable: %v", err)
	}

	p6 := mustPrefix(t, "2001:db8::/64")
	if err := ValidateHostAddress(p6, netip.MustParseAddr("2001:db8::1")); err != nil {
		t.Fatalf("ipv6 host usable: %v", err)
	}
}

func TestNextAvailable(t *testing.T) {
	p := mustPrefix(t, "10.0.0.0/29") // usable .1-.6
	taken := map[string]bool{"10.0.0.1": true, "10.0.0.2": true}
	got := NextAvailable(p, taken, 3)
	want := []string{"10.0.0.3", "10.0.0.4", "10.0.0.5"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}

	// Exhausted subnet returns fewer than requested.
	full := NextAvailable(p, map[string]bool{
		"10.0.0.1": true, "10.0.0.2": true, "10.0.0.3": true,
		"10.0.0.4": true, "10.0.0.5": true, "10.0.0.6": true,
	}, 5)
	if len(full) != 0 {
		t.Fatalf("exhausted subnet should return nothing, got %v", full)
	}

	// /31 both addresses allocatable.
	p31 := mustPrefix(t, "10.0.0.0/31")
	one := NextAvailable(p31, map[string]bool{"10.0.0.0": true}, 5)
	if len(one) != 1 || one[0] != "10.0.0.1" {
		t.Fatalf("/31 next available: got %v", one)
	}
}

func TestOverlapsAny(t *testing.T) {
	existing := []netip.Prefix{
		mustPrefix(t, "10.0.0.0/24"),
		mustPrefix(t, "192.168.1.0/28"),
	}
	if !OverlapsAny(mustPrefix(t, "10.0.0.0/25"), existing) {
		t.Fatal("contained subnet overlaps")
	}
	if !OverlapsAny(mustPrefix(t, "10.0.1.0/23"), existing) {
		t.Fatal("containing subnet overlaps")
	}
	if OverlapsAny(mustPrefix(t, "10.0.2.0/24"), existing) {
		t.Fatal("disjoint subnet must not overlap")
	}
	if OverlapsAny(mustPrefix(t, "192.168.1.16/28"), existing) {
		t.Fatal("adjacent subnet must not overlap")
	}
}

func TestRangeHosts(t *testing.T) {
	hosts, err := RangeHosts(
		netip.MustParseAddr("10.0.0.10"),
		netip.MustParseAddr("10.0.0.12"),
		100,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 || hosts[0] != "10.0.0.10" || hosts[2] != "10.0.0.12" {
		t.Fatalf("unexpected range: %v", hosts)
	}

	if _, err := RangeHosts(netip.MustParseAddr("10.0.0.12"), netip.MustParseAddr("10.0.0.10"), 100); err == nil {
		t.Fatal("reversed range must fail")
	}

	if _, err := RangeHosts(netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.254"), 128); err == nil {
		t.Fatal("oversized range must fail")
	}
}

func TestUtilizationPct(t *testing.T) {
	if UtilizationPct(0, 254) != 0 {
		t.Fatal("empty subnet is 0%")
	}
	if UtilizationPct(127, 254) != 50 {
		t.Fatal("half subnet is 50%")
	}
	if UtilizationPct(300, 254) != 100 {
		t.Fatal("over-allocation clamps to 100%")
	}
	if UtilizationPct(5, 0) != 0 {
		t.Fatal("zero usable must not divide by zero")
	}
}
