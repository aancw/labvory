package dnsx

import (
	"encoding/binary"
	"testing"
)

// buildRR appends an uncompressed resource record to msg.
func buildRR(msg []byte, name string, rtype uint16, ttl uint32, rdata []byte) []byte {
	for _, label := range splitLabels(name) {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	var hdr [10]byte
	binary.BigEndian.PutUint16(hdr[0:2], rtype)
	binary.BigEndian.PutUint16(hdr[2:4], 1)
	binary.BigEndian.PutUint32(hdr[4:8], ttl)
	binary.BigEndian.PutUint16(hdr[8:10], uint16(len(rdata)))
	msg = append(msg, hdr[:]...)
	return append(msg, rdata...)
}

func splitLabels(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				out = append(out, name[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func TestParseMessageWithCompression(t *testing.T) {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x4c56)
	binary.BigEndian.PutUint16(msg[2:4], 0x8400) // response, no error
	binary.BigEndian.PutUint16(msg[4:6], 1)      // qdcount
	binary.BigEndian.PutUint16(msg[6:8], 3)      // ancount

	// Question: example.com AXFR IN
	for _, label := range splitLabels("example.com") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = append(msg, 0x00, 0xfc, 0x00, 0x01)

	// SOA (uncompressed rdata: two names + 5 uint32)
	soaRdata := []byte{}
	for _, label := range splitLabels("ns1.example.com") {
		soaRdata = append(soaRdata, byte(len(label)))
		soaRdata = append(soaRdata, label...)
	}
	soaRdata = append(soaRdata, 0)
	soaRdata = append(soaRdata, make([]byte, 20)...)
	msg = buildRR(msg, "example.com", 6, 3600, soaRdata)

	// A record for host.example.com with a compression pointer to "example.com"
	// (the question name starts at offset 12).
	nameOffset := 12
	aName := []byte{4, 'h', 'o', 's', 't', 0xC0, byte(nameOffset)}
	var aHdr [10]byte
	binary.BigEndian.PutUint16(aHdr[0:2], 1) // A
	binary.BigEndian.PutUint16(aHdr[2:4], 1)
	binary.BigEndian.PutUint32(aHdr[4:8], 300)
	binary.BigEndian.PutUint16(aHdr[8:10], 4)
	msg = append(msg, aName...)
	msg = append(msg, aHdr[:]...)
	msg = append(msg, 10, 0, 0, 42)

	// Closing SOA
	msg = buildRR(msg, "example.com", 6, 3600, soaRdata)

	records, soa, rcode, err := parseMessage(msg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if rcode != 0 {
		t.Fatalf("rcode = %d", rcode)
	}
	if soa != 2 {
		t.Fatalf("soa count = %d, want 2", soa)
	}
	var got *Record
	for i := range records {
		if records[i].Type == "A" {
			got = &records[i]
		}
	}
	if got == nil {
		t.Fatalf("no A record parsed: %+v", records)
	}
	if got.Name != "host.example.com" {
		t.Fatalf("compressed name = %q, want host.example.com", got.Name)
	}
	if got.Value != "10.0.0.42" {
		t.Fatalf("A value = %q", got.Value)
	}
}

func TestDrift(t *testing.T) {
	cases := []struct {
		declared, observed string
		want               bool
	}{
		{"", "1.2.3.4", false},
		{"1.2.3.4", "1.2.3.4", false},
		{"1.2.3.4", "::1, 1.2.3.4", false},
		{"1.2.3.4", "5.6.7.8", true},
		{"mail.example.com", "mail.example.com.", true}, // observed trimmed by resolver, but exact text differs
	}
	for _, c := range cases {
		if got := Drift(c.declared, c.observed); got != c.want {
			t.Errorf("Drift(%q, %q) = %v, want %v", c.declared, c.observed, got, c.want)
		}
	}
}
