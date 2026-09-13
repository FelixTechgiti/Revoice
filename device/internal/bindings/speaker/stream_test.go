package speaker

import "testing"

// The buffering state machine, testable on the host because audioStream is
// untagged. pcm_speaker.go can only be built on the device, which is why
// every one of these behaviours used to be verifiable only by listening.

func newTestStream(depth int) (*audioStream, chan struct{}) {
	dead := make(chan struct{})
	return newAudioStream(depth, dead), dead
}

func pumpN(t *testing.T, s *audioStream, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := s.pump([]byte{byte(i)}, 1); err != nil {
			t.Fatalf("pump %d: %v", i, err)
		}
	}
}

func TestPrimeGateHoldsUntilEnoughIsQueued(t *testing.T) {
	s, _ := newTestStream(64)
	pumpN(t, s, 5)
	if s.ready(24) {
		t.Fatal("should hold on silence until primed")
	}
	pumpN(t, s, 19)
	if !s.ready(24) {
		t.Fatal("should start once primed")
	}
}

func TestAShortClipDoesNotWaitForThePrime(t *testing.T) {
	// Everything it will ever have is already queued; waiting for 24 periods
	// that are never coming would mean it never played.
	s, _ := newTestStream(64)
	pumpN(t, s, 3)
	s.endStream()
	if !s.ready(24) {
		t.Fatal("an ended short clip must play without priming")
	}
}

func TestOncePlayingTheGateStaysOutOfTheWay(t *testing.T) {
	s, _ := newTestStream(64)
	pumpN(t, s, 24)
	s.ready(24)
	s.take()
	if !s.ready(24) {
		t.Fatal("mid-stream playback must not re-prime on every period")
	}
}

func TestANaturalEndReportsStatsRatherThanAnUnderrun(t *testing.T) {
	s, _ := newTestStream(64)
	pumpN(t, s, 2)
	s.endStream()
	s.ready(24)
	s.take()
	s.take()
	st := s.drained()
	if st == nil {
		t.Fatal("a drain after EOS is the end of the stream, not an underrun")
	}
	if st.Periods != 2 {
		t.Fatalf("expected 2 periods, got %d", st.Periods)
	}
	if s.underruns != 0 {
		t.Fatalf("expected no underruns, got %d", s.underruns)
	}
}

func TestAMidStreamDrainIsAnUnderrunNotAnEnding(t *testing.T) {
	s, _ := newTestStream(64)
	pumpN(t, s, 24)
	s.ready(24)
	for i := 0; i < 24; i++ {
		s.take()
	}
	if st := s.drained(); st != nil {
		t.Fatal("the sender falling behind is an underrun, not a completed stream")
	}
	if s.underruns != 1 {
		t.Fatalf("expected 1 underrun, got %d", s.underruns)
	}
}

func TestAFlushedStreamDoesNotLeaveEosArmedForTheNextOne(t *testing.T) {
	// The regression that shipped on 2026-08-03. flush() sets eosPending and
	// the drain that follows consumes it; endStream must NOT set it again, or
	// it stays armed with no stream behind it.
	//
	// Measured symptom: a 2800ms response reported complete after 15 periods
	// (640ms), which ended the turn, cleared the LED ring and released the
	// music duck while the device still held most of the audio.
	s, _ := newTestStream(64)
	pumpN(t, s, 30)
	s.ready(24)
	s.take()

	s.flush()     // barge-in
	s.drained()   // the pump loop sees the emptied channel
	s.endStream() // the cancelled stream's EOS finally arrives

	if s.eosPending.Load() {
		t.Fatal("eosPending must not survive a flushed stream — the next " +
			"stream would report itself complete at its first buffer dip")
	}
}

func TestTheStreamAfterAFlushBehavesNormally(t *testing.T) {
	// The consequence of the bug above, from the next stream's point of view.
	s, _ := newTestStream(64)
	pumpN(t, s, 30)
	s.ready(24)
	s.take()
	s.flush()
	s.drained()
	s.endStream()

	// A fresh response arrives.
	pumpN(t, s, 56)
	if !s.ready(24) {
		t.Fatal("the next stream should prime and play")
	}
	for i := 0; i < 15; i++ {
		s.take()
	}
	// It still has 41 periods queued; nothing should claim it is finished.
	if !s.ready(24) {
		t.Fatal("still has audio — must keep playing")
	}
	if s.eosPending.Load() {
		t.Fatal("no EOS has arrived for this stream")
	}
}

func TestFlushSwallowsTheRestOfTheStreamUntilItsEos(t *testing.T) {
	s, _ := newTestStream(64)
	pumpN(t, s, 10)
	s.flush()
	// The rest of the cancelled response is already in TCP buffers and keeps
	// arriving; it must not refill the channel.
	queued, err := s.pump([]byte{9}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Fatal("post-flush periods must be discarded, not queued")
	}
	s.endStream()
	queued, _ = s.pump([]byte{9}, 1)
	if !queued {
		t.Fatal("the EOS disarms the discard; the next stream must play")
	}
}

func TestMinDepthIgnoresTheTailOfAStream(t *testing.T) {
	// Every stream necessarily drains to zero at its end, so sampling across
	// the tail made this read 0 on 100% of streams, healthy ones included.
	s, _ := newTestStream(64)
	pumpN(t, s, 30)
	s.endStream() // EOS in: everything from here is the tail
	s.ready(24)
	for i := 0; i < 30; i++ {
		s.take()
	}
	if s.minDepth != -1 {
		t.Fatalf("tail periods must not be sampled, got minDepth=%d", s.minDepth)
	}
}

// The bug this file's dropQueue exists for, reproduced as its own story.
//
// Measured on hardware 2026-09-13: Spotify played, its track ended, AirPlay
// took the plane, and nothing came out of the speaker for as long as
// librespot's process lived. Every signal a person would check read healthy.
func TestAFlushWithNoEndOfStreamSwallowsEverythingAfterIt(t *testing.T) {
	s, _ := newTestStream(4)

	// A producer mid-stream.
	if ok, err := s.pump(make([]byte, 4), 4); !ok || err != nil {
		t.Fatalf("first period: ok=%v err=%v", ok, err)
	}
	// It stops, and the firmware drops what is queued.
	s.flush()

	// A DIFFERENT producer takes over and writes. Without an end-of-stream
	// from the first one, this is swallowed — and reported as success, which
	// is the half that makes it invisible.
	ok, err := s.pump(make([]byte, 4), 4)
	if ok {
		t.Fatal("the reproduction failed: the period was not swallowed")
	}
	if err != nil {
		t.Fatalf("and it is swallowed SILENTLY, so err must be nil, got %v", err)
	}
}

func TestDropQueueLeavesTheChannelAbleToPlay(t *testing.T) {
	s, _ := newTestStream(4)
	if ok, _ := s.pump(make([]byte, 4), 4); !ok {
		t.Fatal("setup: first period refused")
	}
	s.dropQueue()

	ok, err := s.pump(make([]byte, 4), 4)
	if !ok || err != nil {
		t.Fatalf("the next producer must be heard: ok=%v err=%v", ok, err)
	}
}

// The half that matters on a device already in the state: it has to recover
// without a restart, because nothing reachable over the network can fix a
// fault in the path the audio takes.
func TestDropQueueRepairsAChannelAlreadyDiscarding(t *testing.T) {
	s, _ := newTestStream(4)
	if ok, _ := s.pump(make([]byte, 4), 4); !ok {
		t.Fatal("setup: first period refused")
	}
	s.flush() // the wrong call, as three call sites used to make it
	if ok, _ := s.pump(make([]byte, 4), 4); ok {
		t.Fatal("setup: expected the channel to be stuck")
	}

	s.dropQueue()

	if ok, err := s.pump(make([]byte, 4), 4); !ok || err != nil {
		t.Fatalf("a stuck channel must recover: ok=%v err=%v", ok, err)
	}
}

// flush keeps its discard, because the controller DOES send an end-of-stream
// and the remainder already in its socket has to be swallowed. Removing that
// would be fixing this bug by reintroducing the one flush was written for.
func TestFlushStillSwallowsTheRemainderItWasWrittenFor(t *testing.T) {
	s, _ := newTestStream(4)
	if ok, _ := s.pump(make([]byte, 4), 4); !ok {
		t.Fatal("setup: first period refused")
	}
	s.flush()
	if ok, _ := s.pump(make([]byte, 4), 4); ok {
		t.Fatal("the network-buffered remainder must still be swallowed")
	}
	s.endStream() // the EOS the controller sends on the cancel path
	if ok, err := s.pump(make([]byte, 4), 4); !ok || err != nil {
		t.Fatalf("and the NEXT stream must play: ok=%v err=%v", ok, err)
	}
}
