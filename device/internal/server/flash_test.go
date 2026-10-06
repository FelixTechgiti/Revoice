package server

import (
	"sync"
	"testing"
	"time"

	"github.com/wilbowes/EchoMuse/pkg/led"
)

type recordingRing struct {
	mu     sync.Mutex
	frames [][]led.Led
}

func (r *recordingRing) Init() error              { return nil }
func (r *recordingRing) GetNumLEDs() (int, error) { return 12, nil }
func (r *recordingRing) SetLEDs(l ...led.Led) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, append([]led.Led(nil), l...))
	return nil
}
func (r *recordingRing) last() led.Led {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frames[len(r.frames)-1][0]
}

func testServer(ring *recordingRing) *Server {
	s := &Server{ledController: ring}
	get := func() led.Controller { return ring }
	s.volume = newVolumeController(get)
	s.mute = newMuteController(get, nil)
	return s
}

func frame(r, g, b uint8) []led.Led {
	l := make([]led.Led, 12)
	for i := range l {
		l[i] = led.Led{ID: i, R: r, G: g, B: b}
	}
	return l
}

// The link pulse repaints every 50ms, so a flash that did not hold the ring
// would last one frame.
func TestFlashHoldsTheRingThenHandsItBack(t *testing.T) {
	ring := &recordingRing{}
	s := testServer(ring)
	s.SetLinkDown(true)

	s.Flash(200, 200, 200, 40*time.Millisecond)
	s.SetLEDs(frame(60, 10, 0), nil) // a pulse frame during the flash
	if got := ring.last(); got.R != 200 || got.G != 200 {
		t.Fatalf("pulse painted over the flash: %+v", got)
	}

	time.Sleep(80 * time.Millisecond)
	if got := ring.last(); got.R != 60 || got.G != 10 {
		t.Fatalf("ring not handed back to the latest frame: %+v", got)
	}
	s.SetLEDs(frame(70, 12, 0), nil)
	if got := ring.last(); got.R != 70 {
		t.Fatalf("paints still held after the flash: %+v", got)
	}
}

// Upstream's version of this test expects the ring to come back RED, because
// upstream's mute ring is the mute indicator. On this fork it is not — the
// action button's own GPIO LED is, and always was, in parallel — so the ring
// belongs to whoever asked for it, muted or not. See suppressPaint.
func TestFlashOnAMutedDeviceReturnsToTheStoredFrame(t *testing.T) {
	ring := &recordingRing{}
	s := testServer(ring)
	s.mute.muted = true
	s.SetLEDs(frame(60, 10, 0), nil)

	s.Flash(200, 200, 200, 20*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if got := ring.last(); got.R != 60 || got.G != 10 {
		t.Fatalf("a muted device must get its own ring back, not red: %+v", got)
	}
}
