package mcast

import (
	"log"
	"strconv"
	"time"
)

// Prober runs the reachability probe on the adaptive cadence, writes the two
// transitions to the log, and — since #142 was explained — repairs the fault it
// finds.
//
// # What the repair is, and why this one
//
// Measured on hardware 2026-09-22, with a `udp dpt:5353` rule present so that
// arrival could be counted rather than inferred: over four minutes the device
// received **no IGMP packet at all** and seven mDNS packets, on a segment where
// twelve hosts answer one `_raop._tcp` query. Then, with nothing else touched:
//
//	VORHER  20s ohne Neubeitritt: +0
//	NACHHER 20s nach Neubeitritt: +579
//
// The mechanism is a lock rather than a fade. The AP snoops IGMP; the device's
// membership is refreshed by answering the querier's general query; that query
// is itself multicast. So once the AP has stopped forwarding the group, the one
// message that would restore it is the one that can no longer arrive, and the
// device stays deaf until something joins the group afresh. Restarting whatever
// holds it is exactly that fresh join, which is why the repair is the same one
// the membership Watcher already performs.
//
// # Why the Prober and not the Watcher
//
// The Watcher reads /proc/net/igmp, and in this fault the membership is
// PRESENT throughout — it is the AP that has let go, and nothing on the device
// records that. The counter is the only instrument that sees it, and the
// counter lives here.
//
// # Why not a socket of our own
//
// Because the Watcher's comment is right: a membership held by the firmware
// makes /proc/net/igmp read healthy for ever and blinds that watcher, while
// doing nothing for the responders, which receive because THEY joined. The
// repair therefore goes through the endpoints, exactly as the Watcher's does.
type Prober struct {
	// Sample reads the firewall's mDNS counter once. Injected so the cadence,
	// the gate and the wording are all host-testable without a firewall.
	Sample func() Reading
	// Active reports whether anything is advertising. Same gate the membership
	// watcher uses and for the same reason: a device with both endpoints off
	// has nothing to be invisible with, and probing it would put a question on
	// somebody's network on behalf of a feature they switched off.
	Active func() bool
	// Repair re-joins the group by restarting whatever holds it — the same
	// closure the membership Watcher is given. Optional: with no repair this
	// is the instrument it has always been.
	Repair func()
	// Busy reports that audio is playing. A repair restarts the endpoints, so
	// running one mid-track stops somebody's music to fix a fault that is not
	// affecting them — a device already streaming has a session, and deafness
	// costs it nothing until that session ends. Optional; absent means never
	// busy, which is right for a caller that cannot tell.
	Busy func() bool
	// RepairEvery bounds it. A repair that does not take would otherwise
	// restart two subprocesses on every deaf window for the life of the
	// process, on a board sharing 512MB with Android — the failure the
	// Watcher's own backoff was added for.
	RepairEvery time.Duration

	Tracker Tracker

	nextRepair time.Time
	repairs    int
}

// DefaultRepairEvery is the shortest gap between two repairs. Long, because the
// fault lasts until something re-joins and a device that has just re-joined
// needs a full deaf window to prove whether it worked.
const DefaultRepairEvery = 10 * time.Minute

// Repairs is how many times this prober has re-joined, for a test and for the
// support bundle.
func (p *Prober) Repairs() int { return p.repairs }

// repair performs the re-join if one is allowed now, and says whether it did.
func (p *Prober) repair(now time.Time) bool {
	if p.Repair == nil {
		return false
	}
	if p.Busy != nil && p.Busy() {
		log.Printf("[mcast] not re-joining while audio is playing — a repair " +
			"restarts the endpoints, and this session is unaffected by being " +
			"deaf. It will be attempted after playback ends.")
		return false
	}
	every := p.RepairEvery
	if every <= 0 {
		every = DefaultRepairEvery
	}
	if !p.nextRepair.IsZero() && now.Before(p.nextRepair) {
		return false
	}
	p.nextRepair = now.Add(every)
	p.repairs++
	log.Printf("[mcast] re-joining the group by restarting the endpoints "+
		"(repair #%d). The membership is present and the link has stopped "+
		"delivering it, which only a fresh join clears — see internal/mcast.",
		p.repairs)
	p.Repair()
	return true
}

// Tick performs one probe and logs a transition if there was one. Returns how
// long to wait before the next probe, so a caller can follow the adaptive
// cadence without duplicating the rule.
func (p *Prober) Tick(now time.Time) time.Duration {
	if p.Sample == nil {
		return HealthyInterval
	}
	if p.Active != nil && !p.Active() {
		// Not a reading. Feeding this to the tracker as silence would report
		// every idle device as deaf, and — worse — the recovery line would then
		// date an outage that was somebody turning a switch back on.
		return HealthyInterval
	}
	r := p.Sample()
	ev, out := p.Tracker.Observe(now, r)
	switch ev {
	case EventDeaf:
		last, delta := p.Tracker.LastHeard()
		// "cannot" is what makes the log relay forward this as a warning, and
		// it has to: the device is perfectly reachable over unicast the whole
		// time, so nothing else in the system reports anything at all.
		//
		// It says what was MEASURED and stops there. An earlier version ended
		// "so it is simply in no picker", which is an inference and was FALSE
		// on hardware: the device reported this while the controller's scan
		// answered "Every enabled endpoint is visible on the network".
		// Announcements go out unprompted and need no query to have arrived.
		log.Printf("[mcast] this device cannot hear the network — not one mDNS "+
			"packet has reached %s:%s in %s (%d consecutive windows). %s "+
			"Unicast is unaffected. Whether it is still VISIBLE is a separate "+
			"question: its own announcements may still be getting out, so read "+
			"the controller's network scan rather than assuming this one. "+
			"Episode #%d.",
			Iface, MDNSPort, out.Round(time.Second), p.Tracker.Misses,
			describeLastHeard(now, last, delta), p.Tracker.Episodes())
	case EventHeard:
		// The all-clear carries the duration, which is the whole point of the
		// pair — a line that only said "working again" would date the recovery
		// and lose the outage.
		_, delta := p.Tracker.LastHeard()
		log.Printf("[mcast] hears the network again: %d mDNS packet(s) arrived "+
			"after %s of silence.", delta, out.Round(time.Second))
	}
	// On the STATE, not on the transition. EventDeaf fires once per episode,
	// and the fault it reports is self-locking: the AP has stopped forwarding
	// the group, and the IGMP query that would restore the membership is
	// itself multicast, so nothing arrives to end the episode on its own. A
	// repair wired to the transition would therefore get exactly one attempt
	// per boot, and a first attempt that did not take would be the last.
	if p.Tracker.Deaf() {
		p.repair(now)
	}
	return p.Tracker.Interval()
}

// describeLastHeard says when mDNS last arrived, or that it never has since
// boot. A device that has just started with a counter that has not moved is a
// different situation from one that was busy a minute ago, and the deaf line is
// read by somebody who has neither in front of them.
func describeLastHeard(now, last time.Time, delta int64) string {
	if last.IsZero() {
		return "The counter has not moved since this firmware started."
	}
	return "Last traffic " + now.Sub(last).Round(time.Second).String() +
		" ago, " + strconv.FormatInt(delta, 10) + " packet(s) in that window."
}

// Run probes until done is closed, on whatever cadence the state calls for.
// A timer rather than a ticker, because the interval changes with the state and
// a ticker would keep the healthy cadence through an outage.
//
// A nil done channel is never ready, which is how the firmware runs it: there
// is nothing to stop this short of the process exiting.
func (p *Prober) Run(done <-chan struct{}) {
	// The first probe waits a cadence rather than firing at startup: the radio
	// has just associated, the endpoints are still being started by the first
	// config push, and a probe into that reports a fault that is really a boot.
	delay := SilentInterval
	t := time.NewTimer(delay)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-t.C:
			t.Reset(p.Tick(now))
		}
	}
}
