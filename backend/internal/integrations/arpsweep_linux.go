//go:build linux

package integrations

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/gopacket/gopacket/afpacket"
	"github.com/gopacket/gopacket/layers"
)

// arpSweepInterface opens AF_PACKET on the interface (pure Go, no cgo) and
// runs the shared sweep. Needs cap_net_raw: docker grants NET_RAW via the
// compose cap_add and the image sets it as a file capability on the binary.
func arpSweepInterface(ctx context.Context, iface net.Interface, srcIP netip.Addr, targets []netip.Addr, timeout time.Duration) (map[string]string, error) {
	tp, err := afpacket.NewTPacket(afpacket.OptInterface(iface.Name))
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "permission denied") {
			return nil, fmt.Errorf("raw packet access denied on %s: grant cap_net_raw to the backend (docker compose already sets NET_RAW + file caps; natively: sudo setcap cap_net_raw+ep or run as root)", iface.Name)
		}
		return nil, fmt.Errorf("open %s for packet sockets: %w", iface.Name, err)
	}
	return arpSweep(ctx, tp, tp, tp, layers.LinkTypeEthernet, iface, srcIP, targets, timeout)
}
