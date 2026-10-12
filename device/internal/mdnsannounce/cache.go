package mdnsannounce

import (
	"bytes"
	"sync"
)

// DefaultMax is how many distinct announcements are kept.
//
// Two responders announce a handful of packets each at startup, so this is
// several times what a healthy device produces. It exists to bound memory
// against a link that is talking, not to shape what is kept.
const DefaultMax = 8

// Cache holds the announcements this device's own responders have made, as
// the bytes they were sent as.
//
// It is written by the listener goroutine and read by the announce ticker, so
// every method takes the lock.
type Cache struct {
	mu   sync.Mutex
	pkts [][]byte
	max  int

	seen     int
	stored   int
	refused  int
	goodbyes int
	resets   int
}

// NewCache returns a cache holding at most max packets; max <= 0 means
// DefaultMax.
func NewCache(max int) *Cache {
	if max <= 0 {
		max = DefaultMax
	}
	return &Cache{max: max}
}

// Observe offers a packet to the cache and reports whether it was kept.
//
// A packet is kept when it is a well-formed response carrying at least one
// answer and no record with TTL 0; a packet carrying one EMPTIES the cache,
// for the reason given at that branch. Duplicates are dropped by exact bytes:
// a responder re-announcing the same records produces the same packet, and
// sending one of them twice a minute is the same as sending it once.
//
// When the cache is full a new packet replaces the stored one carrying the
// FEWEST records, and only if it carries strictly more. The alternative,
// evicting the oldest, loses the startup announcement — the one packet that
// carries PTR, SRV, TXT and A together — in favour of whatever single-record
// answer a browser's last query happened to draw. Keeping the richest is what
// holds a picker entry whole rather than merely listed.
func (c *Cache) Observe(pkt []byte) bool {
	m, err := Inspect(pkt)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen++
	if err != nil || !m.Response || m.Answers == 0 {
		c.refused++
		return false
	}
	if m.Goodbye {
		c.refused++
		c.goodbyes++
		// A goodbye is a responder withdrawing a name (RFC 6762 section 10.1),
		// which it does on its way out. Everything held was announced by one
		// of the two responders on this device, and this says one of them is
		// going — so what is held may already be a claim about a service that
		// has stopped. Dropping all of it rather than trying to match the
		// withdrawn name against stored bytes keeps the failure in the safe
		// direction: this device goes back to being findable only when it can
		// hear a query, which is where it was before this package existed. A
		// ghost in somebody else's picker that accepts a connection to
		// nothing is the other direction, and there is no undoing it from
		// here.
		c.pkts = nil
		return false
	}
	for _, have := range c.pkts {
		if bytes.Equal(have, pkt) {
			return false
		}
	}
	// Copy: the caller owns the read buffer and will fill it again.
	kept := make([]byte, len(pkt))
	copy(kept, pkt)

	if len(c.pkts) < c.max {
		c.pkts = append(c.pkts, kept)
		c.stored++
		return true
	}
	poorest, fewest := -1, m.Records
	for i, have := range c.pkts {
		// Already inspected once when it was stored; re-walking costs
		// microseconds on a packet of a few hundred bytes and saves carrying
		// a parallel slice of counts that could get out of step with pkts.
		hm, err := Inspect(have)
		if err != nil {
			poorest, fewest = i, -1
			break
		}
		if hm.Records < fewest {
			poorest, fewest = i, hm.Records
		}
	}
	if poorest < 0 {
		c.refused++
		return false
	}
	c.pkts[poorest] = kept
	c.stored++
	return true
}

// Packets returns copies of what is held, newest ordering not guaranteed.
func (c *Cache) Packets() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.pkts))
	for i, p := range c.pkts {
		out[i] = append([]byte(nil), p...)
	}
	return out
}

// Len is how many packets would be sent on the next announce.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pkts)
}

// Reset empties the cache.
//
// Called whenever what is held could have stopped being true: an endpoint
// restarted (a rename restarts the receiver, so the old name is in these
// bytes), or the interface took a different address, which is in the A record
// of every announcement here. Repeating a stale address points every browser
// on the LAN at a host that is not us.
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pkts = nil
	c.resets++
}

// Stats reports what the cache has done, for the log line and for a support
// bundle. Goodbyes is the one worth reading: it counts packets that would
// have withdrawn this device from every picker had they been repeated.
func (c *Cache) Stats() (seen, stored, refused, goodbyes, resets int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen, c.stored, c.refused, c.goodbyes, c.resets
}
