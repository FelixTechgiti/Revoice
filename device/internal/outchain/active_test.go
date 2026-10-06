package outchain

import "testing"

// Ported from upstream's own activation test (#243). The API differs — this
// chain works in float64 and crossfades parameter changes — so the assertions
// are rewritten; the three properties they pin are upstream's.

func loud(n int) []float64 {
	x := make([]float64, n)
	for i := range x {
		x[i] = float64((i%97)*300 - 14000)
	}
	return x
}

func equalF(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Inactive is the state every device starts in and stays in under a
// controller that still shapes the audio. It must not touch a single sample.
func TestInactiveIsExactPassthrough(t *testing.T) {
	c := New(48000)
	if c.Active() {
		t.Fatal("a new chain must start inactive — the controller may still be shaping")
	}
	p := DefaultParams()
	p.Bands[0] = 12
	c.SetParams(p)

	in := loud(2048)
	want := append([]float64(nil), in...)
	out := c.Process(in)
	if !equalF(out, want) {
		t.Fatal("an inactive chain changed the audio")
	}
}

// Handing the chain over mid-stream must start clean: state learnt while
// inactive would be state for audio this chain never shaped.
func TestActivationStartsFromCleanState(t *testing.T) {
	p := DefaultParams()
	p.Bands = boostBands(6)

	a := NewChain(48000, p)
	a.SetActive(true)

	b := NewChain(48000, p)
	b.Process(loud(2048)) // inactive: must learn nothing
	b.SetActive(true)

	x, y := a.Process(loud(2048)), b.Process(loud(2048))
	if !equalF(x, y) {
		t.Fatal("activation carried state from an inactive period")
	}
}

// And the gate is reversible: a controller that takes the chain back leaves
// the device passing audio through untouched again.
func TestDeactivationGoesBackToPassthrough(t *testing.T) {
	p := DefaultParams()
	p.Bands = boostBands(6)
	c := NewChain(48000, p)
	c.SetActive(true)
	if shaped := c.Process(loud(2048)); equalF(shaped, loud(2048)) {
		t.Fatal("precondition: an active chain with a +6dB curve must change the audio")
	}
	c.SetActive(false)

	in := loud(2048)
	want := append([]float64(nil), in...)
	if !equalF(c.Process(in), want) {
		t.Fatal("a deactivated chain is still shaping")
	}
}

// TakeStats reports the WORK done and clears its watermark, so a quiet
// reporting window reads as quiet rather than inheriting the last loud one.
func TestTakeStatsReportsAndClears(t *testing.T) {
	p := DefaultParams()
	p.Bands = boostBands(12)
	c := NewChain(48000, p)
	c.SetActive(true)
	for i := 0; i < 20; i++ {
		c.Process(loud(1024))
	}
	st := c.TakeStats()
	if st.LimiterReductionDB <= 0 {
		t.Fatalf("a +12dB curve into the limiter must reduce: %+v", st)
	}
	if st.Clipped != 0 {
		t.Errorf("the limiter is enabled, so nothing may reach the final clip: %d", st.Clipped)
	}
	if again := c.TakeStats(); again.LimiterReductionDB != 0 {
		t.Errorf("the watermark must clear, got %v", again.LimiterReductionDB)
	}
}
