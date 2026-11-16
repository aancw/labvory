//go:build darwin

package integrations

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/gopacket/gopacket/pcap"
)

// arpSweepInterface opens the interface via libpcap (BPF devices) and runs the
// shared sweep. BPF access on macOS is root-only unless the user is in the
// access group, so permission failures get an actionable message.
func arpSweepInterface(ctx context.Context, iface net.Interface, srcIP netip.Addr, targets []netip.Addr, timeout time.Duration) (map[string]string, error) {
	handle, err := pcap.OpenLive(iface.Name, 65536, true, pcap.BlockForever)
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "permission denied") || strings.Contains(msg, "not permitted") || strings.Contains(msg, "operation not permitted") {
			return nil, fmt.Errorf("raw packet access denied on %s: run the backend as root, or grant BPF access once with `sudo dseditgroup -o edit -a %s -t user access` and log in again", iface.Name, os.Getenv("USER"))
		}
		return nil, fmt.Errorf("open %s for capture: %w", iface.Name, err)
	}
	return arpSweep(ctx, handle, handle, handle, handle.LinkType(), iface, srcIP, targets, timeout)
}
