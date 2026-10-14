package mdnsannounce

import (
	"log"
	"time"
)

// DefaultInterval is how often the held announcements go back out.
//
// shairport-sync and librespot publish their SRV, TXT and A records with a
// 120-second TTL, so a browser drops the entry two minutes after the last
// time it heard from us. Forty-five seconds leaves room for two consecutive
// announcements to be lost on the air and the entry still to survive; sixty
// would make a single loss fatal, which is the fault this is here to remove.
// The cost is a handful of packets a minute.
const DefaultInterval = 45 * time.Second

// Announcer re-sends the cached announcements on a cadence.
//
// Everything that touches a socket or the config is a field, so the schedule,
// the invalidation and the gating are testable on a host with no wlan0.
type Announcer struct {
	Cache *Cache

	// Send puts one packet on the mDNS group.
	Send func(pkt []byte) error

	// Identity returns everything the held packets assert about this device:
	// the interface address, which is inside every A record, and the names
	// the endpoints publish, which are inside every PTR, SRV and TXT. Any
	// change makes every held packet a claim about something that no longer
	// exists — a ghost in somebody's picker that connects to nothing, or an
	// address that now belongs to a different host — so the cache is dropped
	// and refilled from whatever the responders announce next.
	//
	// It is ONE value rather than a reset call at each place that restarts an
	// endpoint. There are a dozen of those, a thirteenth is a normal thing to
	// add, and nothing would go red when one forgot: the held packet would
	// simply go on being announced. A plain restart needs no reset at all,
	// because a responder that comes back under the same name announces the
	// same records and the cache keeps one copy of them.
	Identity func() (string, error)

	// Active reports whether anything is published. With both endpoints off
	// there is nothing to announce, and a picker entry that accepts a
	// connection to nothing is worse than no entry.
	Active func() bool

	ident     string
	announced int
	sent      int
	failed    int
	empty     int
	warned    bool
	silent    bool
}

// Tick sends one round and reports how many packets went out.
func (a *Announcer) Tick() int {
	if a.Cache == nil || a.Send == nil {
		return 0
	}
	if a.Active != nil && !a.Active() {
		return 0
	}
	if a.Identity != nil {
		ident, err := a.Identity()
		if err != nil {
			// Failure to look is not evidence — the mcast watcher's rule. An
			// identity that cannot be read is not a changed one, and dropping
			// the cache on it would silence the device for exactly as long as
			// the interface was having trouble.
			return 0
		}
		if a.ident != "" && ident != a.ident {
			log.Printf("[mdnsannounce] this device is now %q and was %q; "+
				"dropping %d held announcement(s), which say otherwise",
				ident, a.ident, a.Cache.Len())
			a.Cache.Reset()
		}
		a.ident = ident
	}
	pkts := a.Cache.Packets()
	if len(pkts) == 0 {
		a.empty++
		// Said once, and only after long enough that it cannot be the gap
		// between this starting and the responders announcing. An empty cache
		// with an endpoint running means the loopback copies never arrived,
		// which is the one way this fails without anything else showing it.
		if !a.silent && a.empty == 8 {
			a.silent = true
			log.Printf("[mdnsannounce] nothing to announce after %d rounds "+
				"with an endpoint running: no announcement from this device's "+
				"own responders has been seen on the mDNS group, so it can "+
				"still only be found by being asked.", a.empty)
		}
		return 0
	}
	a.silent, a.empty = false, 0
	n := 0
	for _, p := range pkts {
		if err := a.Send(p); err != nil {
			a.failed++
			if !a.warned {
				a.warned = true
				log.Printf("[mdnsannounce] could not announce: %v — this "+
					"device stays findable only while it can hear a query", err)
			}
			continue
		}
		n++
	}
	if n > 0 {
		a.warned = false
		a.sent += n
		a.announced++
		// One line an hour at the default interval: enough for a support
		// bundle to answer the next question, which is whether the device was
		// announcing while somebody could not see it.
		//
		// **The cache's REFUSALS ride this line, because without them a cache
		// that is turning announcements away reads exactly like one that has
		// everything there is** (#386). `n` sat at the bound for hours on a
		// device whose Spotify announcement was being refused on every round,
		// and the only number anybody could see was the one that looked
		// healthy. `Cache.Stats` had existed the whole time with no caller.
		if a.announced == 1 || a.announced%80 == 0 {
			seen, stored, refused, goodbyes, resets := a.Cache.Stats()
			log.Printf("[mdnsannounce] announced %d record set(s) unprompted "+
				"(round %d, %d packets sent in total; cache seen=%d stored=%d "+
				"refused=%d goodbyes=%d resets=%d)", n, a.announced, a.sent,
				seen, stored, refused, goodbyes, resets)
		}
	}
	return n
}

// Rounds, Sent and Failures are what the ticker has done.
func (a *Announcer) Rounds() int   { return a.announced }
func (a *Announcer) Sent() int     { return a.sent }
func (a *Announcer) Failures() int { return a.failed }

// Run ticks until done is closed.
//
// The first round waits a full interval rather than firing at once: the
// responders have only just been started, their own announcements are still
// in flight, and repeating them a second later adds nothing a browser acts on.
func (a *Announcer) Run(done <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			a.Tick()
		}
	}
}
