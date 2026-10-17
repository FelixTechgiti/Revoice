package mcast

import "time"

// The second way to become invisible, and the one the membership watcher above
// cannot see: the membership present, both endpoints healthy, and nothing from
// the link arriving at all (#142).
//
// # The instrument this replaces, and why it was wrong
//
// The first version SENT an mDNS query from an ephemeral port with the
// unicast-response (QU) bit set and counted who answered. That reasoning was
// about not disturbing the responders — it never binds 5353 — and it missed the
// platform this runs on.
//
// **On FireOS the replies cannot arrive.** They come from foreign unicast
// addresses to a port no firewall rule names; `-m state --state ESTABLISHED`
// does not match them, because the query went to 224.0.0.251 and the answer
// comes from 192.168.178.x, which is a different flow to conntrack; and the
// chain policy is DROP. So the probe measured the firewall and reported zero
// however healthy the network was.
//
// Measured on hardware 2026-09-12: the warning was in the log while that
// device's own `udp dpt:5353` rule had accepted **93,704 packets**. The device
// was never deaf. `device/tools/mdnsprobe` has the same design and the same
// blind spot, which is why the reading that opened #142 said "heard only
// itself" — that was an artefact, not a network fault.
//
// The lesson is in this repository already, one section along: "Advertised is
// not reachable — FireOS drops every inbound port." Every plane this project
// has is dialled BY the device, so nothing had ever needed an inbound rule, and
// an instrument that quietly needed one was written anyway.
//
// # What it does now
//
// It reads the packet counter on the firewall's own mDNS rule. That cannot be
// fooled by the firewall because it IS the firewall: a rule that accepted a
// packet counted it. It costs one exec, sends nothing, and asks nothing of
// anybody else's network — where the old probe asked every host on the link to
// answer, every cycle, for ever.
//
// # Deaf is not the same as unheard
//
// Announcements go out unprompted, so a responder that hears nothing still
// advertises, and a device can be deaf and listed at once — measured the same
// day, when this warning and the controller's "every enabled endpoint is
// visible" were both true in the same minute. This package measures one
// direction and says so; visibility is `em_mdnsscan`'s question.

const (
	// MDNSPort is the rule whose counter is read.
	MDNSPort = "5353"

	// Iface is where mDNS arrives on this hardware. The rule is per-interface,
	// so this has to agree with what `internal/netfilter` inserts.
	Iface = "wlan0"

	// HealthyInterval / SilentInterval are the adaptive cadence. Reading a
	// counter is one exec and puts nothing on the network, so the only cost is
	// the wake-up; five minutes while healthy, a minute once it looks quiet, so
	// that an outage is dated from its recovery to the minute.
	HealthyInterval = 5 * time.Minute
	SilentInterval  = 60 * time.Second

	// ProbeMisses is how many consecutive windows with no new packet make a
	// device deaf. Two, so a single quiet window on a sleepy network is not an
	// alarm — mDNS chatter on an ordinary LAN runs about a packet a second, so
	// two five-minute windows of absolute silence is a real fault.
	ProbeMisses = 2

	// MinRatePerMin is the fewest packets a minute that counts as hearing the
	// network at all.
	//
	// **The rule used to be absolute silence, and the reasoning above is where
	// it went wrong** (#344): "about a packet a second" is ~300 per window,
	// and the test built on that number fires only at ZERO. Measured
	// 2026-09-24 on one LAN, with the comparison machine associated to the
	// SAME BSSID on the same channel as the device:
	//
	//	a healthy receiver   355 packets in 91s from 30 hosts  ~= 235/min
	//	the device           3 packets in 90s                  ~=   2/min
	//
	// A factor of 117, and the device was invisible to every phone on that
	// network while every panel called it healthy. 6/min sits 39x below what
	// the healthy receiver saw and 3x above what the deaf one did.
	//
	// It is one network's number, which is why it is named rather than
	// inlined. The cost of it being too high is a needless repair on a very
	// quiet LAN — bounded, because the repair is skipped while the device is
	// streaming and rate-limited by RepairEvery. The cost of it being too low
	// is the fault it exists to catch.
	MinRatePerMin = 6.0

	// StarvedMisses is how many consecutive UNDER-RATE windows declare
	// deafness, against ProbeMisses for silent ones.
	//
	// Longer on purpose: a trickle is a weaker signal than silence, and a
	// weaker signal earns a longer look. At the healthy cadence that is 15
	// minutes rather than 10.
	StarvedMisses = 3
)

// Reading is one sample of the counter.
type Reading struct {
	// Packets is the rule's lifetime total. Only its CHANGE is meaningful.
	Packets int64
	// Found says the rule was in the listing at all. A missing rule is not a
	// zero reading: it means the firewall is not in the state we believe, and
	// the sample says nothing about the network.
	Found bool
	// Err is set when the listing could not be read. Failure to look is not
	// evidence of absence — the rule the membership watcher and the wake-word
	// reconcile both follow.
	Err error
}

// Usable reports whether this sample can be compared against another.
func (r Reading) Usable() bool { return r.Err == nil && r.Found }

// Event is a transition worth a log line. Nothing is logged in between: a
// healthy device writes one line when it goes deaf and one when it comes back,
// and a device that has never been deaf writes nothing at all.
type Event int

const (
	EventNone Event = iota
	// EventDeaf — no mDNS packet arrived for the whole of the miss window.
	EventDeaf
	// EventHeard — packets are arriving again. Carries how long the gap was,
	// which is the number #142 is open for.
	EventHeard
)

// Tracker turns a series of counter samples into those two transitions. Pure,
// so the whole decision is host-testable and only the exec is not.
type Tracker struct {
	// Misses is how many consecutive silent windows declare deafness.
	Misses int
	// Starved is how many consecutive UNDER-RATE windows do. Zero takes
	// StarvedMisses, as Misses takes ProbeMisses.
	Starved int
	// MinRate is the floor in packets per minute. Zero takes MinRatePerMin.
	// A NEGATIVE value disables the rate rule and leaves only absolute
	// silence, which is what every Tracker did before #344 — kept expressible
	// so a device on a network this floor is wrong for can be put back.
	MinRate float64

	have      bool  // a comparable baseline exists
	last      int64 // the counter as of the previous usable sample
	lastAt    time.Time
	misses    int
	starved   int
	deaf      bool
	silentAt  time.Time // when the first quiet window of this run was sampled
	lastHeard time.Time
	lastDelta int64
	episodes  int
	resets    int
}

// Observe folds one sample in and reports a transition, if there was one.
//
// The duration reported with EventHeard is measured from the FIRST silent
// sample, not from the one that crossed the miss threshold: the device was
// already quiet then, and the threshold only governs when we are willing to
// say so.
func (t *Tracker) Observe(now time.Time, r Reading) (Event, time.Duration) {
	if t.Misses <= 0 {
		t.Misses = ProbeMisses
	}
	if t.Starved <= 0 {
		t.Starved = StarvedMisses
	}
	if t.MinRate == 0 {
		t.MinRate = MinRatePerMin
	}
	if !r.Usable() {
		return EventNone, 0
	}
	if !t.have {
		t.have, t.last, t.lastAt = true, r.Packets, now
		return EventNone, 0
	}

	// A counter that went DOWN means the rule was re-inserted, which this
	// firmware does on every config push and every repair — `internal/netfilter`
	// deletes and re-inserts rather than trusting `-C`. That is not silence, and
	// reading it as silence would report a fault every time somebody saved a
	// setting. Re-baseline and wait for the next window.
	if r.Packets < t.last {
		t.last, t.lastAt = r.Packets, now
		t.resets++
		return EventNone, 0
	}

	delta := r.Packets - t.last
	elapsed := now.Sub(t.lastAt)
	t.last, t.lastAt = r.Packets, now

	// Three outcomes where there used to be two. A window carrying SOME
	// packets but far too few is starved: the device is hearing a trickle
	// rather than the network, which is the state #344 measured at 3% of what
	// a machine on the same access point saw. It is not silence and must not
	// be read as health either.
	if delta > 0 {
		t.lastDelta = delta
		t.lastHeard = now
		t.misses = 0
	}
	if delta > 0 && !t.starving(delta, elapsed) {
		t.starved = 0
		if t.deaf {
			t.deaf = false
			out := now.Sub(t.silentAt)
			t.silentAt = time.Time{}
			return EventHeard, out
		}
		return EventNone, 0
	}

	// Quiet, one way or the other. The episode is dated from the FIRST quiet
	// window whichever kind it was, because that is when the device stopped
	// hearing the network — the thresholds only govern when we are willing to
	// say so.
	if t.starved == 0 {
		t.silentAt = now
	}
	t.starved++
	if delta == 0 {
		t.misses++
	}
	if t.deaf {
		return EventNone, 0
	}
	if t.misses < t.Misses && t.starved < t.Starved {
		return EventNone, 0
	}
	t.deaf = true
	t.episodes++
	return EventDeaf, now.Sub(t.silentAt)
}

// starving reports whether this window carried too few packets to count as
// hearing the network.
//
// A RATE rather than a per-window count, because the cadence itself changes —
// five minutes while healthy, sixty seconds once quiet — so one fixed count
// would mean two different things. A window of no length cannot be judged, and
// a negative MinRate disables the rule entirely.
func (t *Tracker) starving(delta int64, elapsed time.Duration) bool {
	if t.MinRate < 0 || elapsed <= 0 {
		return false
	}
	return float64(delta) < t.MinRate*elapsed.Minutes()
}

// Deaf reports the current state, which is what chooses the cadence.
func (t *Tracker) Deaf() bool { return t.deaf }

// Episodes is how many times this device has gone deaf since it started — the
// frequency half of what #142 asks for; the duration half rides EventHeard.
func (t *Tracker) Episodes() int { return t.episodes }

// LastHeard is when mDNS last arrived, and how many packets that window
// carried. Zero time means the counter has not moved since this firmware
// started, which on a device that has just booted is not a fault.
func (t *Tracker) LastHeard() (time.Time, int64) { return t.lastHeard, t.lastDelta }

// Interval is the cadence the current state calls for.
func (t *Tracker) Interval() time.Duration {
	if t.deaf {
		return SilentInterval
	}
	return HealthyInterval
}
