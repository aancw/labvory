// Package dnsx provides DNS lookups and a minimal AXFR (zone transfer) client
// built on the standard library, so Lanvory can observe DNS state without
// external dependencies.
package dnsx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ResolveRecord queries DNS for one record type and returns the observed
// answer as a comma-joined string.
func ResolveRecord(ctx context.Context, fqdn, recordType string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r := net.DefaultResolver

	switch strings.ToUpper(recordType) {
	case "A", "AAAA":
		ips, err := r.LookupHost(ctx, fqdn)
		if err != nil {
			return "", err
		}
		var out []string
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if recordType == "A" && ip.To4() != nil {
				out = append(out, ipStr)
			}
			if recordType == "AAAA" && ip.To4() == nil {
				out = append(out, ipStr)
			}
		}
		return strings.Join(out, ", "), nil
	case "CNAME":
		cname, err := r.LookupCNAME(ctx, fqdn)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(cname, "."), nil
	case "MX":
		mxs, err := r.LookupMX(ctx, fqdn)
		if err != nil {
			return "", err
		}
		var out []string
		for _, mx := range mxs {
			out = append(out, strings.TrimSuffix(mx.Host, "."))
		}
		return strings.Join(out, ", "), nil
	case "TXT":
		txts, err := r.LookupTXT(ctx, fqdn)
		if err != nil {
			return "", err
		}
		return strings.Join(txts, " | "), nil
	case "NS":
		nss, err := r.LookupNS(ctx, fqdn)
		if err != nil {
			return "", err
		}
		var out []string
		for _, ns := range nss {
			out = append(out, strings.TrimSuffix(ns.Host, "."))
		}
		return strings.Join(out, ", "), nil
	default:
		return "", errors.New("resolution is not supported for " + recordType + " records; edit the value manually")
	}
}

// Drift reports whether the declared value is absent from the observed answer.
// An empty declared value never drifts.
func Drift(declared, observed string) bool {
	declared = strings.TrimSpace(declared)
	if declared == "" {
		return false
	}
	for _, part := range strings.Split(observed, ",") {
		if strings.EqualFold(strings.TrimSpace(part), declared) {
			return false
		}
	}
	return !strings.EqualFold(strings.TrimSpace(observed), declared)
}

// Record is one resource record returned by a zone transfer.
type Record struct {
	Name  string
	Type  string
	Value string
	TTL   uint32
}

// SOA is the sentinel marker for zone boundaries in a transfer.
const SOA = "SOA"

// Transfer performs a full AXFR of zone from server (host:port). It returns
// every record in the zone except the closing SOA. The server must allow zone
// transfers from this host (BIND allow-transfer, Technitium AXFR, auth-zone in
// dnsmasq, PowerDNS allow-axfr-ips, ...).
func Transfer(ctx context.Context, server, zone string, timeout time.Duration) ([]Record, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, errors.New("AXFR needs config.server (nameserver host:port, e.g. 192.168.1.1:53)")
	}
	if !strings.Contains(server, ":") {
		server += ":53"
	}
	zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
	if zone == "" {
		return nil, errors.New("zone is empty")
	}

	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", server, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	query, err := buildQuery(zone)
	if err != nil {
		return nil, err
	}
	framed := make([]byte, 2+len(query))
	framed[0] = byte(len(query) >> 8)
	framed[1] = byte(len(query))
	copy(framed[2:], query)
	if _, err := conn.Write(framed); err != nil {
		return nil, fmt.Errorf("send query: %w", err)
	}

	var records []Record
	soaSeen := 0
	for soaSeen < 2 {
		msg, err := readMessage(conn)
		if err != nil {
			if soaSeen >= 1 {
				break // server closed after the first SOA; accept partial transfer
			}
			return nil, fmt.Errorf("read response: %w", err)
		}
		recs, soas, rcode, err := parseMessage(msg)
		if err != nil {
			return nil, err
		}
		if rcode != 0 {
			return nil, fmt.Errorf("zone transfer refused by %s (rcode %d) — check allow-transfer/ACL", server, rcode)
		}
		soaSeen += soas
		records = append(records, recs...)
	}
	// Drop the trailing SOA marker, if recorded.
	out := make([]Record, 0, len(records))
	for _, rec := range records {
		if rec.Type == SOA {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func buildQuery(zone string) ([]byte, error) {
	var q []byte
	q = append(q, 0x4c, 0x56) // id "LV"
	q = append(q, 0x00, 0x00) // flags: standard query
	q = append(q, 0x00, 0x01) // qdcount
	q = append(q, 0x00, 0x00) // ancount
	q = append(q, 0x00, 0x00) // nscount
	q = append(q, 0x00, 0x00) // arcount
	for _, label := range strings.Split(zone, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("invalid zone label %q", label)
		}
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0x00)       // root terminator
	q = append(q, 0x00, 0xfc) // QTYPE AXFR (252)
	q = append(q, 0x00, 0x01) // QCLASS IN
	return q, nil
}

func readMessage(conn net.Conn) ([]byte, error) {
	var lenBuf [2]byte
	if _, err := readFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	size := int(lenBuf[0])<<8 | int(lenBuf[1])
	if size < 12 || size > 65535 {
		return nil, fmt.Errorf("invalid DNS message length %d", size)
	}
	msg := make([]byte, size)
	if _, err := readFull(conn, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func parseMessage(msg []byte) (records []Record, soa int, rcode int, err error) {
	if len(msg) < 12 {
		return nil, 0, 0, errors.New("truncated DNS header")
	}
	flags := uint16(msg[2])<<8 | uint16(msg[3])
	rcode = int(flags & 0x000F)
	qd := int(uint16(msg[4])<<8 | uint16(msg[5]))
	an := int(uint16(msg[6])<<8 | uint16(msg[7]))

	off := 12
	for i := 0; i < qd; i++ {
		_, next, err := parseName(msg, off)
		if err != nil {
			return nil, 0, rcode, err
		}
		off = next + 4 // qtype + qclass
	}

	for i := 0; i < an; i++ {
		name, next, err := parseName(msg, off)
		if err != nil {
			return nil, 0, rcode, err
		}
		off = next
		if off+10 > len(msg) {
			return nil, 0, rcode, errors.New("truncated resource record")
		}
		rtype := uint16(msg[off])<<8 | uint16(msg[off+1])
		ttl := uint32(msg[off+4])<<24 | uint32(msg[off+5])<<16 | uint32(msg[off+6])<<8 | uint32(msg[off+7])
		rdlen := int(uint16(msg[off+8])<<8 | uint16(msg[off+9]))
		rdataOff := off + 10
		if rdataOff+rdlen > len(msg) {
			return nil, 0, rcode, errors.New("truncated rdata")
		}
		rec := Record{Name: strings.ToLower(strings.TrimSuffix(name, ".")), TTL: ttl}
		switch rtype {
		case 1: // A
			rec.Type = "A"
			if rdlen == 4 {
				rec.Value = net.IP(msg[rdataOff : rdataOff+4]).String()
			}
		case 28: // AAAA
			rec.Type = "AAAA"
			if rdlen == 16 {
				rec.Value = net.IP(msg[rdataOff : rdataOff+16]).String()
			}
		case 5: // CNAME
			rec.Type = "CNAME"
			if v, _, err := parseName(msg, rdataOff); err == nil {
				rec.Value = strings.TrimSuffix(v, ".")
			}
		case 2: // NS
			rec.Type = "NS"
			if v, _, err := parseName(msg, rdataOff); err == nil {
				rec.Value = strings.TrimSuffix(v, ".")
			}
		case 15: // MX
			rec.Type = "MX"
			if rdlen > 2 {
				pref := uint16(msg[rdataOff])<<8 | uint16(msg[rdataOff+1])
				if v, _, err := parseName(msg, rdataOff+2); err == nil {
					rec.Value = fmt.Sprintf("%d %s", pref, strings.TrimSuffix(v, "."))
				}
			}
		case 16: // TXT
			rec.Type = "TXT"
			var parts []string
			for i := rdataOff; i < rdataOff+rdlen; {
				l := int(msg[i])
				i++
				if i+l > rdataOff+rdlen {
					break
				}
				parts = append(parts, string(msg[i:i+l]))
				i += l
			}
			rec.Value = strings.Join(parts, " ")
		case 33: // SRV
			rec.Type = "SRV"
			if rdlen > 6 {
				prio := uint16(msg[rdataOff])<<8 | uint16(msg[rdataOff+1])
				weight := uint16(msg[rdataOff+2])<<8 | uint16(msg[rdataOff+3])
				port := uint16(msg[rdataOff+4])<<8 | uint16(msg[rdataOff+5])
				if v, _, err := parseName(msg, rdataOff+6); err == nil {
					rec.Value = fmt.Sprintf("%d %d %d %s", prio, weight, port, strings.TrimSuffix(v, "."))
				}
			}
		case 257: // CAA
			rec.Type = "CAA"
		case 6: // SOA
			rec.Type = SOA
			soa++
		default:
			rec.Type = fmt.Sprintf("TYPE%d", rtype)
		}
		off = rdataOff + rdlen
		records = append(records, rec)
	}
	return records, soa, rcode, nil
}

// parseName decodes a (possibly compressed) domain name. next is the offset
// just past the name in the original position.
func parseName(msg []byte, off int) (name string, next int, err error) {
	var labels []string
	p := off
	next = -1
	for {
		if p >= len(msg) {
			return "", 0, errors.New("truncated name")
		}
		c := int(msg[p])
		switch {
		case c == 0:
			p++
			if next < 0 {
				next = p
			}
			return strings.Join(labels, "."), next, nil
		case c&0xC0 == 0xC0:
			if p+1 >= len(msg) {
				return "", 0, errors.New("truncated pointer")
			}
			ptr := int(uint16(msg[p])&0x3F)<<8 | int(msg[p+1])
			if next < 0 {
				next = p + 2
			}
			if ptr >= p {
				return "", 0, errors.New("forward compression pointer")
			}
			p = ptr
		default:
			if p+1+c > len(msg) {
				return "", 0, errors.New("truncated label")
			}
			labels = append(labels, string(msg[p+1:p+1+c]))
			p += 1 + c
		}
	}
}
