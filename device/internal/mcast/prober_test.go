package mcast

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/wilbowes/EchoMuse/internal/logrelay"
)

// capture swaps the process log destination for the length of fn. The wording
// is the deliverable here — this instrument's entire output is two log lines —
// so it is asserted rather than assumed.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(old); log.SetFlags(flags) }()
	fn()
	return buf.String()
}

// counting returns a prober whose counter advances by step each sample.
func counting(step *int64) *Prober {
	var n int64
	return &Prober{
		Active: func() bool { return true },
		Sample: func() Reading { n += *step; return Reading{Packets: n, Found: true} },
	}
}

// THE regression. On hardware 2026-09-12 the old active probe reported this
// device deaf while its own 5353 rule had accepted 93,704 packets — it was
// measuring the firewall, which drops the unicast replies it asked for. A
// moving counter means traffic is arriving, and nothing may call that deaf.
func TestAMovingCounterIsNeverDeaf(t *testing.T) {
	step := int64(300) // ~a packet a second across a five-minute window
	p := counting(&step)
	now := time.Now()
	out := capture(t, func() {
		for i := 0; i < 40; i++ {
			p.Tick(now.Add(time.Duration(i) * HealthyInterval))
		}
	})
	if p.Tracker.Deaf() {
		t.Error("a device receiving 300 packets per window was reported deaf")
	}
	if out != "" {
		t.Errorf("a healthy device logged:\n%s", out)
	}
}

func TestAStuckCounterIsDeafAfterTheMissWindow(t *testing.T) {
	var tr Tracker
	now := time.Now()
	base := Reading{Packets: 93704, Found: true}
	tr.Observe(now, base) // baseline
	if ev, _ := tr.Observe(now.Add(5*time.Minute), base); ev != EventNone {
		t.Fatal("one silent window already declared deafness")
	}
	ev, out := tr.Observe(now.Add(10*time.Minute), base)
	if ev != EventDeaf {
		t.Fatalf("two silent windows produced %v", ev)
	}
	if out != 5*time.Minute {
		t.Errorf("the outage measured %s, want 5m — dated from the FIRST silent "+
			"sample, not from the one that crossed the threshold", out)
	}
}

// The one way a counter lies. Every config push and every firewall repair
// deletes and re-inserts the rule, which zeroes it. Reading that as silence
// would report a fault every time somebody saved a setting.
func TestARuleReinsertionIsNotSilence(t *testing.T) {
	var tr Tracker
	now := time.Now()
	tr.Observe(now, Reading{Packets: 93704, Found: true})
	for i := 1; i <= 6; i++ {
		// Counter restarts from zero and climbs again.
		ev, _ := tr.Observe(now.Add(time.Duration(i)*time.Minute),
			Reading{Packets: int64(i * 40), Found: true})
		if ev != EventNone {
			t.Fatalf("a re-inserted rule produced %v at step %d", ev, i)
		}
	}
	if tr.Deaf() {
		t.Error("a rule re-insertion was counted as an outage")
	}
}

// Failure to look is not evidence of absence, and a MISSING rule is not a zero
// reading — it means the firewall is not in the state we believe.
func TestAnUnreadableOrAbsentRuleIsNotSilence(t *testing.T) {
	for _, r := range []Reading{
		{Err: errors.New("no iptables on this device")},
		{Packets: 0, Found: false},
	} {
		var tr Tracker
		now := time.Now()
		tr.Observe(now, Reading{Packets: 10, Found: true})
		for i := 1; i <= 10; i++ {
			if ev, _ := tr.Observe(now.Add(time.Duration(i)*time.Minute), r); ev != EventNone {
				t.Fatalf("%+v produced %v", r, ev)
			}
		}
		if tr.Deaf() {
			t.Errorf("%+v was counted as an outage", r)
		}
	}
}

// THE contract that makes this instrument worth anything: the device is
// reachable over unicast throughout, so the only person who can see the fault
// is somebody reading their Home Assistant log, and the line gets there by
// matching the relay's failure markers.
func TestTheDeafLineReachesTheController(t *testing.T) {
	var step int64
	p := counting(&step) // never advances
	now := time.Now()
	out := capture(t, func() {
		p.Tick(now)
		p.Tick(now.Add(5 * time.Minute))
		p.Tick(now.Add(10 * time.Minute))
	})
	if out == "" {
		t.Fatal("a stuck counter logged nothing")
	}
	level, fwd := logrelay.Classify(out)
	if !fwd || level != logrelay.LevelWarn {
		t.Fatalf("the deaf line is not relayed as a warning (level=%q fwd=%v):\n%s",
			level, fwd, out)
	}
}

// And the all-clear too, or the log holds every onset and no recovery — which
// reads as every outage still running.
func TestTheRecoveryLineReachesTheControllerWithTheDuration(t *testing.T) {
	var step int64
	p := counting(&step)
	now := time.Now()
	p.Tick(now)
	p.Tick(now.Add(5 * time.Minute))
	p.Tick(now.Add(10 * time.Minute)) // deaf here
	step = 412
	out := capture(t, func() { p.Tick(now.Add(35 * time.Minute)) })
	level, fwd := logrelay.Classify(out)
	if !fwd {
		t.Fatalf("the recovery line is not relayed at all:\n%s", out)
	}
	if level != logrelay.LevelInfo {
		t.Errorf("the recovery line is relayed as %q; it is not a failure", level)
	}
	if !strings.Contains(out, "30m0s") {
		t.Errorf("the recovery line does not carry the outage length:\n%s", out)
	}
	if !strings.Contains(out, "412") {
		t.Errorf("the recovery line does not say how much arrived:\n%s", out)
	}
}

// Hearing and being heard are two directions, and the line must not infer one
// from the other. Measured on hardware: the device logged the deaf line while
// the controller's scan answered "Every enabled endpoint is visible".
func TestTheDeafLineDoesNotClaimTheDeviceIsInvisible(t *testing.T) {
	var step int64
	p := counting(&step)
	now := time.Now()
	out := capture(t, func() {
		p.Tick(now)
		p.Tick(now.Add(5 * time.Minute))
		p.Tick(now.Add(10 * time.Minute))
	})
	for _, claim := range []string{"in no picker", "not visible", "invisible"} {
		if strings.Contains(strings.ToLower(out), claim) {
			t.Errorf("the deaf line asserts %q, which it does not measure:\n%s", claim, out)
		}
	}
	if !strings.Contains(out, "scan") {
		t.Errorf("the deaf line does not send the reader to the scan that "+
			"answers visibility:\n%s", out)
	}
}

// A device with both endpoints off has nothing to be invisible with, and
// counting the result would report every idle device as deaf.
func TestNothingIsSampledWhileNothingIsAdvertised(t *testing.T) {
	sampled := 0
	p := &Prober{
		Sample: func() Reading { sampled++; return Reading{Found: true} },
		Active: func() bool { return false },
	}
	now := time.Now()
	out := capture(t, func() {
		for i := 0; i < 10; i++ {
			p.Tick(now.Add(time.Duration(i) * time.Minute))
		}
	})
	if sampled != 0 {
		t.Errorf("%d samples taken with both endpoints off", sampled)
	}
	if p.Tracker.Deaf() || out != "" {
		t.Errorf("an idle device was recorded as deaf or logged:\n%s", out)
	}
}

// A deaf device logs once, not once per window: the relay is rationed at six
// lines a minute on the liveness channel. Same shape as #404.
func TestAnOutageLogsOnceHoweverLongItLasts(t *testing.T) {
	var step int64
	p := counting(&step)
	now := time.Now()
	out := capture(t, func() {
		for i := 0; i < 60; i++ {
			p.Tick(now.Add(time.Duration(i) * time.Minute))
		}
	})
	if n := strings.Count(out, "cannot hear"); n != 1 {
		t.Errorf("an hour of deafness produced %d warnings", n)
	}
}

// Tick returns the cadence so a caller cannot hold the healthy interval through
// an outage, which would date every recovery to the nearest five minutes.
func TestTickReportsTheCadenceTheStateCallsFor(t *testing.T) {
	var step int64
	p := counting(&step)
	now := time.Now()
	if d := p.Tick(now); d != HealthyInterval {
		t.Errorf("the baseline sample asked for %s, want %s", d, HealthyInterval)
	}
	p.Tick(now.Add(5 * time.Minute))
	if d := p.Tick(now.Add(10 * time.Minute)); d != SilentInterval {
		t.Errorf("a deaf device asked for %s, want %s", d, SilentInterval)
	}
}

func TestEpisodesCountsEachOutageSeparately(t *testing.T) {
	var tr Tracker
	now := time.Now()
	var n int64 = 1000
	step := func(adv int64) {
		now = now.Add(time.Minute)
		n += adv
		tr.Observe(now, Reading{Packets: n, Found: true})
	}
	step(0) // baseline
	for i := 0; i < 2; i++ {
		step(0)
		step(0)
		step(50)
	}
	if tr.Episodes() != 2 {
		t.Errorf("%d episodes for two outages", tr.Episodes())
	}
}

// The cadence is what an outage's resolution depends on.
func TestTheSilentCadenceIsTheFastOne(t *testing.T) {
	if SilentInterval >= HealthyInterval {
		t.Fatalf("silent=%s is not faster than healthy=%s", SilentInterval, HealthyInterval)
	}
}

// deafProber returns a Prober that always reads a stalled counter, plus the
// repair count it drives.
func deafProber(t *testing.T) (*Prober, *int) {
	t.Helper()
	n := 0
	p := &Prober{
		Sample: func() Reading { return Reading{Packets: 7, Found: true} },
		Repair: func() { n++ },
	}
	return p, &n
}

// Walk the prober far enough to be deaf, then on for the requested span.
func run(p *Prober, from time.Time, span, step time.Duration) {
	for d := time.Duration(0); d <= span; d += step {
		p.Tick(from.Add(d))
	}
}

func TestADeafProberRejoins(t *testing.T) {
	p, n := deafProber(t)
	run(p, time.Unix(0, 0), 30*time.Minute, time.Minute)
	if *n == 0 {
		t.Fatal("stayed deaf for half an hour and never re-joined — this is the " +
			"whole of #142, and the fault does not clear on its own")
	}
}

func TestRepairKeepsTryingWhileDeaf(t *testing.T) {
	// EventDeaf fires once per episode and the fault is self-locking, so a
	// repair hung on the transition gets one attempt per boot. If the first
	// restart does not take, that is a device deaf until somebody reboots it.
	p, n := deafProber(t)
	p.RepairEvery = 5 * time.Minute
	run(p, time.Unix(0, 0), 60*time.Minute, time.Minute)
	if *n < 3 {
		t.Fatalf("re-joined %d times in an hour of deafness, wanted several — "+
			"a single attempt per episode is one attempt per boot", *n)
	}
}

func TestRepairIsBounded(t *testing.T) {
	// The Watcher's backoff exists because restarting two subprocesses every
	// interval on a board sharing 512MB with Android is a fault of its own.
	p, n := deafProber(t)
	p.RepairEvery = 10 * time.Minute
	run(p, time.Unix(0, 0), 30*time.Minute, 10*time.Second)
	if *n > 4 {
		t.Fatalf("re-joined %d times in 30 minutes with a 10-minute bound", *n)
	}
}

func TestNoRepairWhileAudioIsPlaying(t *testing.T) {
	// A repair restarts the endpoints. Deafness costs a session in progress
	// nothing — it already has its connection — so cutting somebody's music to
	// fix it is the repair being worse than the fault.
	p, n := deafProber(t)
	p.Busy = func() bool { return true }
	run(p, time.Unix(0, 0), 60*time.Minute, time.Minute)
	if *n != 0 {
		t.Fatalf("restarted the endpoints %d times during playback", *n)
	}
}

func TestPlaybackDefersRatherThanCancels(t *testing.T) {
	playing := true
	n := 0
	p := &Prober{
		Sample: func() Reading { return Reading{Packets: 7, Found: true} },
		Repair: func() { n++ },
		Busy:   func() bool { return playing },
	}
	base := time.Unix(0, 0)
	run(p, base, 30*time.Minute, time.Minute)
	if n != 0 {
		t.Fatal("repaired while busy")
	}
	playing = false
	run(p, base.Add(31*time.Minute), 30*time.Minute, time.Minute)
	if n == 0 {
		t.Fatal("never repaired after playback ended — a deferred repair that " +
			"is dropped leaves the device deaf until the next reboot")
	}
}

func TestAProberWithNoRepairIsStillTheInstrument(t *testing.T) {
	// The repair is optional, and a caller that supplies none must keep the
	// logging behaviour this package had before it.
	p := &Prober{Sample: func() Reading { return Reading{Packets: 7, Found: true} }}
	run(p, time.Unix(0, 0), 30*time.Minute, time.Minute)
	if p.Repairs() != 0 {
		t.Fatalf("counted %d repairs with no Repair set", p.Repairs())
	}
	if !p.Tracker.Deaf() {
		t.Fatal("did not notice the stalled counter")
	}
}

func TestAHealthyProberNeverRepairs(t *testing.T) {
	n, packets := 0, int64(0)
	p := &Prober{
		Sample: func() Reading { packets += 50; return Reading{Packets: packets, Found: true} },
		Repair: func() { n++ },
	}
	run(p, time.Unix(0, 0), 60*time.Minute, time.Minute)
	if n != 0 {
		t.Fatalf("restarted the endpoints %d times on a healthy device", n)
	}
}

func TestNoRuleMeansNoRepair(t *testing.T) {
	// The counter only exists where `internal/netfilter` has written the mDNS
	// rule, and there is firmware and emOS in the field without it (#298). A
	// missing rule must read as "cannot tell", never as silence — reading it
	// as silence would restart both endpoints every ten minutes for the life
	// of the process on every device that has not been updated yet, which is
	// a worse fault than the one being repaired and would arrive as part of
	// the fix for it.
	n := 0
	p := &Prober{
		Sample: func() Reading { return Reading{Found: false} },
		Repair: func() { n++ },
	}
	run(p, time.Unix(0, 0), 60*time.Minute, time.Minute)
	if n != 0 {
		t.Fatalf("restarted the endpoints %d times on a device whose mDNS rule "+
			"could not be read at all", n)
	}
	if p.Tracker.Deaf() {
		t.Fatal("an unreadable counter was recorded as deafness")
	}
}

func TestAFailedReadIsNotSilence(t *testing.T) {
	n := 0
	p := &Prober{
		Sample: func() Reading { return Reading{Err: errors.New("iptables: not found")} },
		Repair: func() { n++ },
	}
	run(p, time.Unix(0, 0), 60*time.Minute, time.Minute)
	if n != 0 {
		t.Fatalf("restarted the endpoints %d times because the firewall could "+
			"not be read", n)
	}
}
