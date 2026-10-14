package mdnsannounce

import (
	"bytes"
	"testing"

	"github.com/miekg/dns"
)

// records builds a response carrying n PTR answers, so richness can be varied
// without varying anything else.
func records(n int, name string) []byte {
	m := new(dns.Msg)
	m.Response = true
	for i := 0; i < n; i++ {
		m.Answer = append(m.Answer, &dns.PTR{
			Hdr: dns.RR_Header{Name: svc, Rrtype: dns.TypePTR,
				Class: dns.ClassINET, Ttl: 4500},
			Ptr: string(rune('a'+i)) + name,
		})
	}
	b, err := m.Pack()
	if err != nil {
		panic(err)
	}
	return b
}

// A responder announces the same records repeatedly — RFC 6762 section 8.3
// asks for at least two, a second apart. Keeping both and sending both would
// double the traffic for no second entry anywhere.
func TestTheSameAnnouncementTwiceIsKeptOnce(t *testing.T) {
	c := NewCache(0)
	pkt := announcement(120, "192.168.178.42")
	if !c.Observe(pkt) {
		t.Fatal("the first announcement was not kept")
	}
	if c.Observe(pkt) {
		t.Error("the same bytes were kept a second time")
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
}

// The caller reads into one buffer for the life of the socket, so anything
// kept has to be a copy.
func TestWhatIsKeptSurvivesTheCallersBufferBeingReused(t *testing.T) {
	c := NewCache(0)
	buf := make([]byte, 0, 512)
	buf = append(buf, announcement(120, "192.168.178.42")...)
	c.Observe(buf)
	want := append([]byte(nil), buf...)
	for i := range buf {
		buf[i] = 0xFF
	}
	got := c.Packets()
	if len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Fatal("the stored packet changed when the read buffer was refilled")
	}
}

// And Packets hands out copies, so a sender that retries in place cannot
// corrupt what will be sent next time.
func TestPacketsHandsOutCopies(t *testing.T) {
	c := NewCache(0)
	c.Observe(announcement(120, "192.168.178.42"))
	got := c.Packets()
	got[0][0] = 0xFF
	if bytes.Equal(c.Packets()[0], got[0]) {
		t.Error("a caller writing to a returned packet changed the cache")
	}
}

// A full cache keeps the richest packets. The startup announcement carries
// PTR, SRV, TXT and A together and is what holds a picker entry whole; a
// single-answer reply to somebody's query keeps only the name. Evicting the
// oldest would trade the first for the second within minutes of boot.
func TestAFullCacheGivesUpItsPoorestPacketAndNotItsOldest(t *testing.T) {
	c := NewCache(2)
	rich := records(5, ".rich.local.")
	poor := records(1, ".poor.local.")
	if !c.Observe(rich) || !c.Observe(poor) {
		t.Fatal("the cache did not fill")
	}
	richer := records(7, ".richer.local.")
	if !c.Observe(richer) {
		t.Fatal("a richer packet was refused by a full cache")
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
	held := c.Packets()
	for _, want := range [][]byte{rich, richer} {
		found := false
		for _, h := range held {
			if bytes.Equal(h, want) {
				found = true
			}
		}
		if !found {
			t.Error("a richer packet was dropped in favour of a poorer one")
		}
	}
	for _, h := range held {
		if bytes.Equal(h, poor) {
			t.Error("the single-answer packet survived a full cache")
		}
	}
}

func TestAFullCacheRefusesAPacketPoorerThanEverythingInIt(t *testing.T) {
	c := NewCache(2)
	c.Observe(records(5, ".a.local."))
	c.Observe(records(5, ".b.local."))
	if c.Observe(records(2, ".c.local.")) {
		t.Error("a poorer packet displaced a richer one")
	}
}

// A rename restarts the receiver, and the old name is in these bytes. So is
// the old address. Either makes everything held a lie, and the firmware says
// so by resetting rather than by trying to work out which packet aged.
func TestResetEmptiesTheCacheAndIsCounted(t *testing.T) {
	c := NewCache(0)
	c.Observe(announcement(120, "192.168.178.42"))
	c.Reset()
	if c.Len() != 0 {
		t.Errorf("Len = %d after Reset", c.Len())
	}
	if _, _, _, _, resets := c.Stats(); resets != 1 {
		t.Errorf("resets = %d, want 1", resets)
	}
}

func TestRubbishOffTheLinkIsRefusedWithoutPanicking(t *testing.T) {
	c := NewCache(0)
	for _, b := range [][]byte{
		nil,
		{},
		{0x00},
		bytes.Repeat([]byte{0xFF}, 64),
		bytes.Repeat([]byte{0x00}, HeaderLen),
	} {
		if c.Observe(b) {
			t.Errorf("%v was stored", b)
		}
	}
	if c.Len() != 0 {
		t.Errorf("Len = %d, want 0", c.Len())
	}
}

// ─── The bound may not decide WHICH services are kept (#386) ─────────────────
//
// The eviction below the bound keeps the RICHEST packet, which is right within
// one service — a startup announcement carrying PTR, SRV, TXT and A together
// holds a picker entry whole, where a browser's single-record answer does not.
// Across services it is blind: a leaner responder's announcement can never
// displace a richer one, so once the cache is full of one endpoint's packets
// the other endpoint is excluded permanently rather than merely crowded.
//
// That is what 8 did on a device running both receivers. The fix is that the
// bound is large enough to never be reached by this device's own responders,
// so the policy never has to arbitrate between services at all — which is why
// these tests are about the BOUND and not about the eviction.

func TestALeanAnnouncementSurvivesARicherResponder(t *testing.T) {
	// shairport-sync announces two service types with full record sets;
	// librespot announces one, leaner. The lean one must still be held.
	c := NewCache(0)
	for i := 0; i < 8; i++ {
		if !c.Observe(records(6, "rich"+string(rune('a'+i))+".local.")) {
			t.Fatalf("rich announcement %d was refused", i)
		}
	}
	if !c.Observe(records(3, "lean.local.")) {
		_, stored, refused, _, _ := c.Stats()
		t.Fatalf("the lean announcement was refused — the bound is deciding "+
			"which services are kept (stored=%d refused=%d)", stored, refused)
	}
}

func TestTheBoundIsNotReachedByThisDevicesOwnResponders(t *testing.T) {
	// Since #385 the listener admits only packets sourced from this device,
	// so nothing but our own responders can fill this. Three service types
	// today — _raop._tcp, _airplay._tcp, _spotify-connect._tcp — and RFC 6762
	// section 8.3 asks a responder to repeat an announcement at least twice.
	// A fourth endpoint must not be the thing that reintroduces #386.
	const services, repeats, headroom = 4, 2, 2
	if want := services * repeats * headroom; DefaultMax < want {
		t.Fatalf("DefaultMax = %d, which leaves no room for %d services "+
			"announcing %d times; #386 is one endpoint away from returning",
			DefaultMax, services, repeats)
	}
}
