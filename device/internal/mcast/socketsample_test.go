package mcast

import (
	"errors"
	"testing"
)

func TestAForeignCountBecomesAComparableReading(t *testing.T) {
	r := SocketSample(func() (int64, bool) { return 17, true })()
	if !r.Usable() {
		t.Fatalf("a count from a live socket must be comparable, got %+v", r)
	}
	if r.Packets != 17 {
		t.Fatalf("Packets = %d, want 17", r.Packets)
	}
}

// The whole point of the adapter: no socket is an ABSENT sample, never a
// silent one. Reading it as zero is what reports a device deaf on the strength
// of never having looked at it.
func TestNoSocketIsNotAReadingOfZero(t *testing.T) {
	r := SocketSample(func() (int64, bool) { return 0, false })()
	if r.Usable() {
		t.Fatal("a missing socket must not produce a comparable reading")
	}
	if !errors.Is(r.Err, ErrNoSocket) {
		t.Fatalf("Err = %v, want ErrNoSocket", r.Err)
	}
	if r.Found {
		t.Fatal("Found must stay false, or the Tracker would baseline on it")
	}
}

func TestNoCounterAtAllIsTheSameAbsence(t *testing.T) {
	if r := SocketSample(nil)(); r.Usable() {
		t.Fatal("a nil counter must not produce a comparable reading")
	}
}

// A genuine zero IS a measurement, and the Tracker has to be allowed to see it
// — that is how a deaf device is found at all. Easy to lose while making the
// case above safe.
func TestAGenuineZeroIsStillAMeasurement(t *testing.T) {
	r := SocketSample(func() (int64, bool) { return 0, true })()
	if !r.Usable() {
		t.Fatal("zero packets from a live socket is the deaf reading, not an absent one")
	}
}

// The Tracker goes deaf only on repeated samples that do not MOVE, so an
// adapter that lost the count between calls would hide every real outage.
func TestTheCountIsReadPerSampleRatherThanOnce(t *testing.T) {
	n := int64(0)
	sample := SocketSample(func() (int64, bool) { n++; return n, true })
	if first := sample(); first.Packets != 1 {
		t.Fatalf("first read = %d, want 1", first.Packets)
	}
	if second := sample(); second.Packets != 2 {
		t.Fatalf("second read = %d, want 2 — the counter is re-read per sample", second.Packets)
	}
}
