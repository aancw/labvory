package integrations

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

// Active ARP sweep (in-process, à la gopacket's arpscan example): craft ARP
// requests on the wire, collect replies, map ip → mac. No external binary and
// no kernel neighbor-table read — both of which macOS silently denies to
// spawned helper binaries.
//
// The packet socket is platform-specific: pcap (cgo, system libpcap) on darwin,
// pure-Go afpacket on linux. See arpsweep_darwin.go / arpsweep_linux.go.

// maxAutoSweepHosts caps per-interface sweeps chosen automatically: a /24 is
// expected; bigger prefixes (or /8 loopback-style prefixes) are refused unless
// the operator names the CIDR explicitly.
const maxAutoSweepHosts = 1024

// maxExplicitSweepHosts is the hard ceiling even for an explicit CIDR.
const maxExplicitSweepHosts = 65536

// packetWriter is the subset of a packet socket used to send crafted frames.
type packetWriter interface {
	WritePacketData([]byte) error
}

// packetCloser matches both pcap.Handle and afpacket.TPacket, whose Close
// methods return nothing.
type packetCloser interface {
	Close()
}

type sweepTarget struct {
	iface  net.Interface
	ip     netip.Addr   // source IP to claim in the requests
	prefix netip.Prefix // the subnet being swept
	hosts  []netip.Addr // addresses to probe
}

// activeInterfaceAddr returns the source IPv4 the kernel would use to reach
// the internet — a UDP "connect" performs no I/O, it only consults the routing
// table, so it names the active (default-route) interface without privileges.
func activeInterfaceAddr() (netip.Addr, error) {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 2*time.Second)
	if err != nil {
		return netip.Addr{}, err
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil {
		return netip.Addr{}, fmt.Errorf("could not determine the local address of the default route")
	}
	addr, ok := netip.AddrFromSlice(local.IP)
	if !ok {
		return netip.Addr{}, fmt.Errorf("the default route is not IPv4")
	}
	return addr.Unmap(), nil
}

// arpSweepTargets resolves what to sweep: the ACTIVE interface (the one the
// default route leaves through) unless an interface is named; cidr overrides
// that interface's own subnet.
func arpSweepTargets(ifaceName, cidr string, maxHosts int) ([]sweepTarget, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate interfaces: %w", err)
	}
	if ifaceName != "" {
		for _, ifc := range ifaces {
			if ifc.Name != ifaceName {
				continue
			}
			targets, err := targetsForInterface(ifc, cidr, maxAutoSweepHosts)
			if err != nil {
				return nil, err
			}
			if len(targets) == 0 {
				return nil, fmt.Errorf("interface %s has no sweepable IPv4 subnet", ifaceName)
			}
			return targets, nil
		}
		return nil, fmt.Errorf("interface %q not found", ifaceName)
	}

	activeIP, err := activeInterfaceAddr()
	if err != nil {
		return nil, fmt.Errorf("could not determine the active (default-route) interface: %w — set the interface explicitly in the connector config", err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) == 0 {
			continue
		}
		addrs, aerr := ifc.Addrs()
		if aerr != nil {
			continue
		}
		match := false
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipn.IP)
			if ok && addr.Unmap() == activeIP {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		targets, err := targetsForInterface(ifc, cidr, maxHosts)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			return nil, fmt.Errorf("interface %s has no sweepable IPv4 subnet", ifc.Name)
		}
		return targets, nil
	}
	return nil, fmt.Errorf("the active interface (source address %s) was not found among system interfaces", activeIP)
}

// targetsForInterface builds the sweep for one interface. cidr overrides the
// interface's own subnet (and lifts the auto cap to maxHosts).
func targetsForInterface(ifc net.Interface, cidr string, maxHosts int) ([]sweepTarget, error) {
	if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
		return nil, nil
	}
	if len(ifc.HardwareAddr) == 0 {
		return nil, nil // virtual tunnels without a MAC cannot send ARP
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil, nil
	}
	var ip netip.Addr
	var prefix netip.Prefix
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipn.IP)
		if !ok {
			continue
		}
		// macOS getifaddrs hands out IPv4 as 4-in-6 (Is4() false) — unmap to
		// plain IPv4 before the checks below.
		addr = addr.Unmap()
		if !addr.Is4() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
			continue
		}
		ones, _ := ipn.Mask.Size()
		ip = addr
		prefix = netip.PrefixFrom(addr, ones)
		break
	}
	if !ip.IsValid() {
		return nil, nil
	}

	var swept netip.Prefix
	var hosts []netip.Addr
	if cidr != "" {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid cidr %q: %w", cidr, err)
		}
		if !p.Addr().Is4() {
			return nil, fmt.Errorf("cidr %q must be IPv4", cidr)
		}
		swept = p
		hosts = expandHosts(p, maxExplicitSweepHosts)
		if hosts == nil {
			return nil, fmt.Errorf("cidr %q is too large (max %d hosts)", cidr, maxExplicitSweepHosts)
		}
	} else {
		swept = prefix.Masked() // auto-created subnets want the network address
		hosts = expandHosts(prefix, maxHosts)
	}
	if len(hosts) == 0 {
		return nil, nil
	}
	return []sweepTarget{{iface: ifc, ip: ip, prefix: swept, hosts: hosts}}, nil
}

// expandHosts lists the probeable addresses of a prefix (network and broadcast
// excluded), capped at max. Returns nil for prefixes that are single IPs or
// exceed the cap.
func expandHosts(prefix netip.Prefix, max int) []netip.Addr {
	base := prefix.Masked().Addr()
	if !base.Is4() || prefix.IsSingleIP() {
		return nil
	}
	hostBits := prefix.Addr().BitLen() - prefix.Bits()
	if hostBits < 2 {
		return nil // /31 and /32 have no network+broadcast pair to exclude
	}
	size := uint64(1) << uint(hostBits)
	if size-2 > uint64(max) {
		return nil
	}
	base4 := base.As4()
	baseU := binary.BigEndian.Uint32(base4[:])
	start := baseU + 1              // first host
	end := baseU + uint32(size) - 2 // last host (broadcast - 1)
	out := make([]netip.Addr, 0, size-2)
	for a := start; a <= end; a++ {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], a)
		out = append(out, netip.AddrFrom4(b))
	}
	return out
}

// arpSweepInterface runs one sweep on the given interface. Implemented
// per-platform (pcap on darwin, afpacket on linux).

// arpGroupByMac groups discovered entries by MAC; the last sighting wins.
func arpGroupByMac(entries []arpEntry) map[string]string {
	out := map[string]string{}
	for _, e := range entries {
		if e.MAC == "" || !e.IP.IsValid() {
			continue
		}
		out[e.MAC] = e.IP.String()
	}
	return out
}

// arpSweep sends one ARP request per target and collects replies until the
// timeout. Requests are sent first (replies buffer in the kernel), then the
// reader runs; closing the handle unblocks it.
func arpSweep(ctx context.Context, src gopacket.PacketDataSource, writer packetWriter, closer packetCloser,
	linkType layers.LinkType, iface net.Interface, srcIP netip.Addr, targets []netip.Addr, timeout time.Duration) (map[string]string, error) {

	defer closer.Close()
	targetSet := map[string]bool{}
	for _, t := range targets {
		targetSet[t.String()] = true
	}
	srcMAC := iface.HardwareAddr
	ip4 := srcIP.As4()

	buf := gopacket.NewSerializeBuffer()
	broadcast := net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	for _, t := range targets {
		t4 := t.As4()
		eth := &layers.Ethernet{
			SrcMAC:       srcMAC,
			DstMAC:       broadcast,
			EthernetType: layers.EthernetTypeARP,
		}
		ar := &layers.ARP{
			AddrType:          layers.LinkTypeEthernet,
			Protocol:          layers.EthernetTypeIPv4,
			HwAddressSize:     6,
			ProtAddressSize:   4,
			Operation:         layers.ARPRequest,
			SourceHwAddress:   []byte(srcMAC),
			SourceProtAddress: []byte(ip4[:]),
			DstHwAddress:      make([]byte, 6),
			DstProtAddress:    []byte(t4[:]),
		}
		if err := gopacket.SerializeLayers(buf, gopacket.SerializeOptions{}, eth, ar); err != nil {
			return nil, fmt.Errorf("serialize arp request for %s: %w", t, err)
		}
		if err := writer.WritePacketData(buf.Bytes()); err != nil {
			return nil, fmt.Errorf("send arp request to %s: %w", t, err)
		}
		time.Sleep(time.Millisecond) // pace one class-C to well under a second
	}

	packets := make(chan gopacket.Packet, 128)
	go func() {
		defer close(packets)
		source := gopacket.NewPacketSource(src, linkType)
		for packet := range source.Packets() {
			packets <- packet
		} // Close() (deferred by the caller) unblocks this read
	}()

	results := map[string]string{}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		case <-timer.C:
			return results, nil
		case pkt, ok := <-packets:
			if !ok {
				return results, nil
			}
			al := pkt.Layer(layers.LayerTypeARP)
			if al == nil {
				continue
			}
			arp, ok := al.(*layers.ARP)
			if !ok || arp.Operation != layers.ARPReply {
				continue
			}
			key := net.IP(arp.SourceProtAddress).String()
			if !targetSet[key] {
				continue
			}
			if _, seen := results[key]; !seen {
				mac := net.HardwareAddr(arp.SourceHwAddress).String() // lowercase colon form
				if mac != "" && isUnicastMAC(mac) {
					results[key] = mac
				}
			}
		}
	}
}
