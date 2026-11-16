// Package ipam implements pure subnet/address math for Lanvory.
// All functions are side-effect free so they can be exhaustively unit-tested;
// the database layer composes them with transactions.
package ipam

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net/netip"
	"strings"
)

var (
	ErrInvalidCIDR    = errors.New("invalid CIDR notation")
	ErrOutsideSubnet  = errors.New("address is outside the subnet")
	ErrNetworkAddress = errors.New("address is the network address")
	ErrBroadcast      = errors.New("address is the broadcast address")
	ErrRangeInvalid   = errors.New("range start must be before or equal to end")
)

// ParseCIDR parses and normalizes a CIDR string to its network form.
func ParseCIDR(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%w: %v", ErrInvalidCIDR, err)
	}
	return p.Masked(), nil
}

// UsableCount returns the number of usable host addresses.
// IPv4: network and broadcast are reserved, except /31 (RFC 3021: both usable)
// and /32 (single host). IPv6: every address is usable.
// Counts saturate at MaxUint64 for enormous prefixes (e.g. IPv6 /64+).
func UsableCount(p netip.Prefix) uint64 {
	hostBits := p.Addr().BitLen() - p.Bits()
	if p.Addr().Is4() {
		switch p.Bits() {
		case 31:
			return 2
		case 32:
			return 1
		}
		if hostBits >= 64 {
			return math.MaxUint64
		}
		return uint64(1)<<uint(hostBits) - 2
	}
	if hostBits >= 64 {
		return math.MaxUint64
	}
	return uint64(1) << uint(hostBits)
}

// UsableRange returns the first and last usable host addresses (inclusive).
// IPv4 reserves network and broadcast for prefixes ≤/30; /31 (RFC 3021) and /32
// use every address. For IPv6, the whole prefix range is usable.
func UsableRange(p netip.Prefix) (netip.Addr, netip.Addr) {
	first := p.Masked().Addr()
	if first.Is4() {
		b := first.As4()
		network := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		hostBits := 32 - p.Bits()
		var hostMask uint32
		if hostBits < 32 {
			hostMask = (uint32(1) << uint(hostBits)) - 1
		}
		broadcast := network | hostMask

		switch p.Bits() {
		case 32:
			return first, first
		case 31: // both addresses usable
		default:
			network++
			broadcast--
		}
		first = netip.AddrFrom4([4]byte{
			byte(network >> 24), byte(network >> 16 & 0xff),
			byte(network >> 8 & 0xff), byte(network & 0xff),
		})
		last := netip.AddrFrom4([4]byte{
			byte(broadcast >> 24), byte(broadcast >> 16 & 0xff),
			byte(broadcast >> 8 & 0xff), byte(broadcast & 0xff),
		})
		return first, last
	}

	// IPv6: every address in the prefix is usable.
	b := first.As16()
	hostBits := 128 - p.Bits()
	lastBytes := b
	if hostBits >= 128 {
		for i := range lastBytes {
			lastBytes[i] = 0xff
		}
	} else {
		fullBytes := hostBits / 8
		remBits := hostBits % 8
		for i := 15; i >= 16-int(fullBytes); i-- {
			lastBytes[i] = 0xff
		}
		if remBits > 0 && 16-int(fullBytes)-1 >= 0 {
			i := 16 - int(fullBytes) - 1
			lastBytes[i] |= byte(1<<uint(remBits)) - 1
		}
	}
	last, _ := netip.AddrFromSlice(lastBytes[:])
	return first, last
}

// Contains reports whether addr is inside the prefix.
func Contains(p netip.Prefix, addr netip.Addr) bool {
	return p.Contains(addr.Unmap())
}

// ValidateHostAddress checks that addr sits inside p and is a usable host address
// (not the network or broadcast address for IPv4 networks that reserve them).
func ValidateHostAddress(p netip.Prefix, addr netip.Addr) error {
	if !Contains(p, addr) {
		return ErrOutsideSubnet
	}
	if !addr.Is4() {
		return nil // IPv6 has no reserved network/broadcast
	}
	first, _ := p.Masked().Addr().Prefix(32)
	_ = first
	if p.Bits() == 32 {
		return nil
	}
	if addr == p.Masked().Addr() && p.Bits() <= 30 {
		return ErrNetworkAddress
	}
	_, lastAll := UsableRange(p)
	broadcast := lastAll
	if p.Bits() <= 30 {
		// UsableRange already excluded broadcast; reconstruct it.
		b4 := p.Masked().Addr().As4()
		hostBits := 32 - p.Bits()
		var hostMask uint32 = (uint32(1) << uint(hostBits)) - 1
		network := uint32(b4[0])<<24 | uint32(b4[1])<<16 | uint32(b4[2])<<8 | uint32(b4[3])
		broadcast = netip.AddrFrom4([4]byte{
			byte((network | hostMask) >> 24), byte((network | hostMask) >> 16 & 0xff),
			byte((network | hostMask) >> 8 & 0xff), byte((network | hostMask) & 0xff),
		})
	}
	if addr == broadcast && p.Bits() <= 30 {
		return ErrBroadcast
	}
	return nil
}

// NextAvailable returns up to n unallocated addresses in the subnet, scanning
// upward from the first usable address. taken holds allocated addresses in
// string form (as stored in PostgreSQL inet output).
func NextAvailable(p netip.Prefix, taken map[string]bool, n int) []string {
	if n <= 0 {
		return nil
	}
	first, last := UsableRange(p)
	out := make([]string, 0, n)
	cur := first
	for i := uint64(0); i < UsableCount(p); i++ {
		s := cur.String()
		if !taken[s] {
			out = append(out, s)
			if len(out) >= n {
				return out
			}
		}
		if cur == last {
			break
		}
		cur = cur.Next()
		if !cur.IsValid() {
			break
		}
	}
	return out
}

// OverlapsAny reports whether candidate overlaps any of the existing CIDRs.
func OverlapsAny(candidate netip.Prefix, existing []netip.Prefix) bool {
	for _, e := range existing {
		if candidate.Overlaps(e) {
			return true
		}
	}
	return false
}

// RangeHosts expands an inclusive address range to its host strings,
// bounded by `max` to protect the API from absurd ranges.
func RangeHosts(start, end netip.Addr, max int) ([]string, error) {
	if start.Compare(end) > 0 {
		return nil, ErrRangeInvalid
	}
	out := make([]string, 0, 16)
	cur := start
	for i := 0; i < max; i++ {
		out = append(out, cur.String())
		if cur.Compare(end) == 0 {
			return out, nil
		}
		cur = cur.Next()
		if !cur.IsValid() {
			return nil, ErrRangeInvalid
		}
	}
	return nil, fmt.Errorf("range exceeds %d addresses", max)
}

// UtilizationPct computes allocation percentage (0-100), rounded to nearest.
func UtilizationPct(used, usable uint64) int {
	if usable == 0 {
		return 0
	}
	if used >= usable {
		return 100
	}
	pct := int(math.Round(float64(used) / float64(usable) * 100))
	if pct > 100 {
		return 100
	}
	if pct < 0 {
		return 0
	}
	return pct
}

// popcount helper kept for future network-math needs.
var _ = bits.OnesCount32
