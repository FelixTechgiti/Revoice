package mcast

import (
	"testing"
	"time"
)

// ── Deafness is a RATE, not a zero (#344) ───────────────────────────────────

func read(n int64) Reading { return Reading{Packets: n, Found: true} }

// The numbers are the ones measured on hardware: a healthy receiver on the
// same access point saw ~235 packets a minute, the device saw ~2. The old rule
// called the device healthy because two is not zero.
func TestTheMeasuredTrickleIsDeafness(t *testing.T) {
	var tr Tracker
	now := time.Unix(0, 0)
	// ~2 packets a minute, which is what the device actually received.
	n := int64(1000)
	tr.Observe(now, read(n))
	var deaf bool
	for i := 0; i < 6; i++ {
		now = now.Add(HealthyInterval)
		n += 10 // 10 per five minutes
		if ev, _ := tr.Observe(now, read(n)); ev == EventDeaf {
			deaf = true
			break
		}
	}
	if !deaf {
		t.Fatal("a device at 3% of the LAN's mDNS rate was never called deaf")
	}
}

func TestTheMeasuredHealthyRateIsNeverDeafness(t *testing.T) {
	var tr Tracker
	now := time.Unix(0, 0)
	n := int64(1000)
	tr.Observe(now, read(n))
	for i := 0; i < 12; i++ {
		now = now.Add(HealthyInterval)
		n += 1174 // what a healthy receiver saw per five-minute window
		if ev, _ := tr.Observe(now, read(n)); ev != EventNone {
			t.Fatalf("a healthy device produced %v", ev)
		}
	}
	if tr.Deaf() {
		t.Fatal("a healthy device reads as deaf")
	}
}

func TestSilenceIsStillFasterThanATrickle(t *testing.T) {
	// A weaker signal earns a longer look: absolute silence keeps its
	// two-window rule, a trickle takes three.
	silent := func() int {
		var tr Tracker
		now := time.Unix(0, 0)
		tr.Observe(now, read(500))
		for i := 1; i <= 8; i++ {
			now = now.Add(HealthyInterval)
			if ev, _ := tr.Observe(now, read(500)); ev == EventDeaf {
				return i
			}
		}
		return -1
	}()
	trickle := func() int {
		var tr Tracker
		now := time.Unix(0, 0)
		n := int64(500)
		tr.Observe(now, read(n))
		for i := 1; i <= 8; i++ {
			now = now.Add(HealthyInterval)
			n += 5
			if ev, _ := tr.Observe(now, read(n)); ev == EventDeaf {
				return i
			}
		}
		return -1
	}()
	if silent != ProbeMisses {
		t.Fatalf("silence declared deaf after %d windows, want %d", silent, ProbeMisses)
	}
	if trickle != StarvedMisses {
		t.Fatalf("a trickle declared deaf after %d windows, want %d", trickle, StarvedMisses)
	}
	if !(trickle > silent) {
		t.Fatal("a trickle must take LONGER than silence — it is the weaker signal")
	}
}

func TestTheFloorIsARateAndNotAWindowCount(t *testing.T) {
	// The cadence changes: five minutes while healthy, sixty seconds once
	// quiet. A fixed per-window count would mean two different things.
	var tr Tracker
	tr.have, tr.last, tr.lastAt = true, 0, time.Unix(0, 0)
	tr.MinRate = MinRatePerMin
	// 6/min for one minute is 6 — not starving at exactly the floor.
	if tr.starving(6, time.Minute) {
		t.Fatal("exactly at the floor read as starving")
	}
	// The same 6 packets across five minutes is well under it.
	if !tr.starving(6, 5*time.Minute) {
		t.Fatal("the floor is being applied per window rather than per minute")
	}
}

func TestANegativeFloorRestoresTheOldRule(t *testing.T) {
	// Kept expressible so a device on a network this floor is wrong for can
	// be put back to absolute silence.
	var tr Tracker
	tr.MinRate = -1
	now := time.Unix(0, 0)
	n := int64(100)
	tr.Observe(now, read(n))
	for i := 0; i < 8; i++ {
		now = now.Add(HealthyInterval)
		n += 1
		if ev, _ := tr.Observe(now, read(n)); ev == EventDeaf {
			t.Fatal("a negative floor still applied the rate rule")
		}
	}
}
