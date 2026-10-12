package mdnsannounce

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// The packets below are built with github.com/miekg/dns rather than written
// out by hand. The point of this file is that our skip-walk agrees with a
// conforming encoder about where a record ends — checking it against bytes we
// also wrote would only prove we are consistent with ourselves. miekg/dns
// compresses names, which is the case that matters: every announcement from
// either responder carries the service name four times.

const (
	svc  = "_raop._tcp.local."
	inst = "A1B2C3D4E5F6@Studio._raop._tcp.local."
	host = "Studio.local."
)

// announcement is the shape shairport-sync and librespot both send at
// startup: one PTR answer, with SRV, TXT and A in the additional section.
func announcement(ttl uint32, addr string) []byte {
	m := new(dns.Msg)
	m.Response = true
	m.Authoritative = true
	m.Answer = []dns.RR{
		&dns.PTR{Hdr: dns.RR_Header{Name: svc, Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: ttl}, Ptr: inst},
	}
	m.Extra = []dns.RR{
		&dns.SRV{Hdr: dns.RR_Header{Name: inst, Rrtype: dns.TypeSRV,
			Class: dns.ClassINET, Ttl: ttl}, Port: 7000, Target: host},
		&dns.TXT{Hdr: dns.RR_Header{Name: inst, Rrtype: dns.TypeTXT,
			Class: dns.ClassINET, Ttl: ttl},
			Txt: []string{"pk=0a1b", "ft=0x4A7FCA00,0xBC354BD0", "vs=366"}},
		&dns.A{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeA,
			Class: dns.ClassINET, Ttl: ttl}, A: mustIP(addr)},
	}
	m.Compress = true
	b, err := m.Pack()
	if err != nil {
		panic(err)
	}
	return b
}

func mustIP(s string) net.IP {
	ip := net.ParseIP(s).To4()
	if ip == nil {
		panic("not an IPv4 address: " + s)
	}
	return ip
}

func TestAConformingAnnouncementIsReadTheWayItWasPacked(t *testing.T) {
	pkt := announcement(120, "192.168.178.42")
	m, err := Inspect(pkt)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !m.Response {
		t.Error("an announcement is a response and did not read as one")
	}
	if m.Answers != 1 {
		t.Errorf("Answers = %d, want 1", m.Answers)
	}
	if m.Records != 4 {
		t.Errorf("Records = %d, want 4 (PTR + SRV + TXT + A)", m.Records)
	}
	if m.Goodbye {
		t.Error("a TTL of 120 read as a goodbye")
	}
}

// RFC 6762 section 10.1: a responder withdraws a name by repeating the record
// with a TTL of 0. Repeating one of those every 45 seconds would hold this
// device out of every picker on the network for as long as the firmware ran.
func TestAGoodbyeIsRecognisedAndNeverRepeated(t *testing.T) {
	m, err := Inspect(announcement(0, "192.168.178.42"))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !m.Goodbye {
		t.Fatal("a TTL of 0 did not read as a goodbye")
	}
	c := NewCache(0)
	if !c.Observe(announcement(120, "192.168.178.42")) {
		t.Fatal("the live announcement was not kept")
	}
	if c.Observe(announcement(0, "192.168.178.42")) {
		t.Fatal("a goodbye was stored for repeating")
	}
	if _, _, _, goodbyes, _ := c.Stats(); goodbyes != 1 {
		t.Errorf("goodbyes = %d, want 1", goodbyes)
	}
	// And it takes the rest with it: a responder on its way out means what is
	// held may already describe a service that has stopped.
	if c.Len() != 0 {
		t.Errorf("Len = %d after a goodbye, want 0", c.Len())
	}
}

// Only ONE record has to carry the zero. A responder withdrawing its address
// while leaving the service listed sends exactly that.
func TestOneZeroTTLAmongLiveRecordsIsStillAGoodbye(t *testing.T) {
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{
		&dns.PTR{Hdr: dns.RR_Header{Name: svc, Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: 4500}, Ptr: inst},
	}
	m.Extra = []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Name: host, Rrtype: dns.TypeA,
			Class: dns.ClassINET, Ttl: 0}, A: mustIP("192.168.178.42")},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Inspect(b)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !got.Goodbye {
		t.Error("a zero TTL in the additional section was missed")
	}
}

func TestAQueryIsNotSomethingToRepeat(t *testing.T) {
	m := new(dns.Msg)
	m.SetQuestion(svc, dns.TypePTR)
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Inspect(b)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Response {
		t.Error("a query read as a response")
	}
	if NewCache(0).Observe(b) {
		t.Error("a query was stored for repeating")
	}
}

// RFC 6762 section 6 allows a response to carry the question it answers. It
// is still an announcement, and the question section has to be walked before
// the records are reached.
func TestAResponseThatRepeatsItsQuestionIsStillWalked(t *testing.T) {
	m := new(dns.Msg)
	m.SetQuestion(svc, dns.TypePTR)
	m.Response = true
	m.Answer = []dns.RR{
		&dns.PTR{Hdr: dns.RR_Header{Name: svc, Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: 4500}, Ptr: inst},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Inspect(b)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !got.Response || got.Answers != 1 || got.Records != 1 {
		t.Errorf("Inspect = %+v, want a response with one answer", got)
	}
}

// A response with nothing in it is legal and says nothing. Repeating it costs
// a packet and refreshes no entry anywhere.
func TestAnEmptyResponseIsNotWorthRepeating(t *testing.T) {
	m := new(dns.Msg)
	m.Response = true
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if NewCache(0).Observe(b) {
		t.Error("a response with no answer was stored")
	}
}

// Every truncation of a real packet must come back as an error. A walk that
// ran off the end and returned what it had would send a fragment of somebody
// else's record set to the whole network.
func TestEveryTruncationOfARealPacketIsRefused(t *testing.T) {
	pkt := announcement(120, "192.168.178.42")
	for n := 0; n < len(pkt); n++ {
		if _, err := Inspect(pkt[:n]); err == nil {
			t.Fatalf("a %d-byte prefix of a %d-byte announcement was accepted",
				n, len(pkt))
		}
	}
	if _, err := Inspect(pkt); err != nil {
		t.Fatalf("the whole packet was refused: %v", err)
	}
}

// RFC 1035 section 4.1.4 assigns 00 (a label) and 11 (a pointer) and leaves
// 01 and 10 reserved. Nothing has ever been assigned to them, so guessing a
// length is how a walk desynchronises and reads a TTL out of record data.
func TestTheReservedLabelTypesAreAnError(t *testing.T) {
	for _, b := range []byte{0x40, 0x80} {
		pkt := make([]byte, HeaderLen+8)
		binary.BigEndian.PutUint16(pkt[2:4], 0x8400) // response, authoritative
		binary.BigEndian.PutUint16(pkt[6:8], 1)      // one answer
		pkt[HeaderLen] = b
		if _, err := Inspect(pkt); err == nil {
			t.Errorf("label type 0x%02x was accepted", b)
		}
	}
}

func TestAShortMessageSaysSoRatherThanPanicking(t *testing.T) {
	for n := 0; n < HeaderLen; n++ {
		_, err := Inspect(make([]byte, n))
		if !errors.Is(err, ErrShort) {
			t.Errorf("Inspect(%d bytes) = %v, want ErrShort", n, err)
		}
	}
}

// A label is at most 63 bytes and a name at most 255 (RFC 1035 section 2.3.4);
// the longest instance name a responder can publish is therefore still walked
// by length and never by searching for a terminator.
func TestANameOfMaximumLengthIsWalked(t *testing.T) {
	long := strings.Repeat("x", 63) + "." + strings.Repeat("y", 63) + "." +
		strings.Repeat("z", 63) + "._raop._tcp.local."
	m := new(dns.Msg)
	m.Response = true
	m.Answer = []dns.RR{
		&dns.PTR{Hdr: dns.RR_Header{Name: svc, Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: 4500}, Ptr: long},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(b); err != nil {
		t.Fatalf("a name at the limit was refused: %v", err)
	}
}
