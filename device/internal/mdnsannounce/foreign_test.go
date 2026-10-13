package mdnsannounce

import (
	"errors"
	"net"
	"testing"

	"github.com/miekg/dns"
)

// answer builds a well-formed announcement, so Observe has something it will
// actually keep — a packet the cache rejects would make the "ours is cached"
// assertions pass for the wrong reason.
func answer(t *testing.T) []byte {
	t.Helper()
	m := new(dns.Msg)
	m.Response = true
	m.Answer = append(m.Answer, &dns.PTR{
		Hdr: dns.RR_Header{
			Name: "_raop._tcp.local.", Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: 120,
		},
		Ptr: "FCA183C69315@Studio._raop._tcp.local.",
	})
	b, err := m.Pack()
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return b
}

func ours() (string, error) { return "192.168.178.140", nil }

// listening gives the Listener a real socket so the tests go through
// ForeignCount rather than round the back of it — the `ok` it returns is part
// of what they are checking. A plain unicast socket on the loopback is enough:
// `note` never touches the wire, and joining a group in a test container is
// exactly what this avoids.
func listening(t *testing.T) *Listener {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("a loopback UDP socket is the one thing this test needs: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &Listener{conn: conn}
}

// The fault this counter exists for. The firewall's mDNS rule matches on the
// destination, so our own announcements — delivered back through the kernel's
// multicast loopback — moved the counter the deafness probe read, and the
// probe's own repair is what produced them.
func TestOurOwnAnnouncementIsCachedAndNeverCounted(t *testing.T) {
	l := listening(t)
	c := NewCache(0)
	l.note(c, answer(t), net.ParseIP("192.168.178.140"), ours)

	if n, _ := l.ForeignCount(); n != 0 {
		t.Fatalf("our own packet counted as foreign (%d) — this is #384", n)
	}
	if c.Len() != 1 {
		t.Fatalf("our own announcement must still be cached, Len = %d", c.Len())
	}
}

func TestSomebodyElsesPacketIsCountedAndNeverCached(t *testing.T) {
	l := listening(t)
	c := NewCache(0)
	l.note(c, answer(t), net.ParseIP("192.168.178.23"), ours)

	if n, _ := l.ForeignCount(); n != 1 {
		t.Fatalf("ForeignCount = %d, want 1", n)
	}
	if c.Len() != 0 {
		t.Fatal("somebody else's records must never enter our announcement cache")
	}
}

// Failure to look is not evidence, and here it would be evidence of exactly
// the wrong thing: an unreadable address would make every arrival foreign and
// report a deaf device as hearing the whole LAN.
func TestAnUnreadableAddressCountsNothing(t *testing.T) {
	l := listening(t)
	c := NewCache(0)
	broken := func() (string, error) { return "", errors.New("no address") }
	l.note(c, answer(t), net.ParseIP("192.168.178.23"), broken)
	l.note(c, answer(t), nil, ours)

	if n, _ := l.ForeignCount(); n != 0 {
		t.Fatalf("ForeignCount = %d, want 0 — an undecidable origin is neither", n)
	}
	if c.Len() != 0 {
		t.Fatal("an undecidable origin must not be cached either")
	}
}

func TestTheCountAccumulatesAcrossArrivals(t *testing.T) {
	l := listening(t)
	c := NewCache(0)
	for _, ip := range []string{"192.168.178.23", "192.168.178.24", "192.168.178.23"} {
		l.note(c, answer(t), net.ParseIP(ip), ours)
	}
	if n, _ := l.ForeignCount(); n != 3 {
		t.Fatalf("ForeignCount = %d, want 3", n)
	}
}

// No socket is not a count of zero. The probe must report nothing rather than
// report a device deaf it has never been able to listen to.
func TestNoSocketReportsAbsenceRatherThanSilence(t *testing.T) {
	var none *Listener
	if n, ok := none.ForeignCount(); ok || n != 0 {
		t.Fatalf("nil listener: got (%d, %v), want (0, false)", n, ok)
	}
	if n, ok := (&Listener{}).ForeignCount(); ok || n != 0 {
		t.Fatalf("unopened listener: got (%d, %v), want (0, false)", n, ok)
	}
}
