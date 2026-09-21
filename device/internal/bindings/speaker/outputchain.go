//go:build server

package speaker

import (
	"encoding/binary"
	"sync"

	"github.com/wilbowes/EchoMuse/internal/outchain"
)

// The output chain's attachment to the ALSA write path.
//
// Everything with an opinion about audio lives in internal/outchain, which is
// pure Go with no build tag and is verified against the controller's Python
// bit for bit. THIS file is the part that cannot be tested on the host, so it
// deliberately contains no DSP: buffer conversion, the mono assumption, and
// when to run.

// chainState is the speaker's handle on the output chain. Separate from
// PcmSpeaker's other fields because it is written from the control plane and
// read on the ALSA goroutine.
type chainState struct {
	mu    sync.Mutex
	chain *outchain.Chain

	// cfg is the last parameter set the controller pushed, held UNRESOLVED.
	//
	// The chain runs cfg.ForJack(plug position), and the plug position moves
	// on its own — so the pushed set has to be kept to re-resolve against,
	// and holding only the resolved one would make an unplug irreversible.
	cfg     outchain.Params
	haveCfg bool

	// mono is the de-interleaved working buffer, and stereoOut is where a
	// processed period is assembled. Both are owned by the ALSA goroutine.
	mono      []float64
	stereoOut []byte

	// drain counts periods to keep processing after the audio stops.
	//
	// The limiter delays by its look-ahead and carries the remainder in a
	// tail, so when a response ends its last ~5ms is still inside the chain.
	// Stopping the moment the mixer goes quiet would strand those samples
	// until the NEXT stream, which would then start with the tail of the
	// previous one — a real artefact, and an obscure one to diagnose.
	//
	// One period (2048 samples) far exceeds the look-ahead (240), so a single
	// extra period always suffices. Running the chain continuously would also
	// work and costs CPU on an idle device for nothing.
	drain int
}

// SetOutputChain installs or updates the chain's parameters.
//
// Safe to call from the control plane at any time: parameter changes are
// crossfaded by outchain.Chain rather than applied at a period boundary, so
// this cannot click however often the controller pushes.
//
// Takes the pushed set UNRESOLVED and resolves it here, because half the
// inputs are not the controller's: the plug position is read off the jack and
// changes without any config push (#231).
func (p *PcmSpeaker) SetOutputChain(params outchain.Params) {
	p.oc.mu.Lock()
	p.oc.cfg = params
	p.oc.haveCfg = true
	p.oc.mu.Unlock()
	p.applyOutputChainParams()
}

// applyOutputChainParams resolves the pushed parameters against the current
// plug position and hands the result to the chain. The single place the chain
// is given parameters, so resolution cannot be skipped on one path and applied
// on the other.
//
// Reached from two directions — SetOutputChain when the controller pushes, and
// SetJackRouting when a plug moves. A no-op until the controller has pushed
// once, and cheap when the resolved set has not moved: outchain.Chain compares
// before it crossfades, so a jack transition on a device with the bypass off
// costs one comparison.
//
// SetJackRouting is also what covers a device BOOTED with a cable in, because
// jack.Watch dispatches the state it starts in rather than only transitions.
func (p *PcmSpeaker) applyOutputChainParams() {
	// Read the plug position OUTSIDE the chain lock: SetJackRouting holds
	// jackMu around its own write and calls in here afterwards, and taking
	// the two in different orders on different paths is how a deadlock gets
	// built.
	p.jackMu.Lock()
	inserted := p.jackKnown && p.jackInserted
	p.jackMu.Unlock()

	p.oc.mu.Lock()
	defer p.oc.mu.Unlock()
	if !p.oc.haveCfg {
		return
	}
	params := p.oc.cfg.ForJack(inserted)
	if p.oc.chain == nil {
		p.oc.chain = outchain.NewChain(sampleRate, params)
		return
	}
	p.oc.chain.SetParams(params)
}

// applyOutputChain runs one mixed period through the chain.
//
// THE MONO ASSUMPTION. The wire carries mono and PumpPeriod duplicates L=R
// before queueing, so both channels of a mixed period are identical by
// construction. This processes channel 0 and mirrors it, which is exact while
// that holds and halves the work.
//
// It stops holding the day the jack carries real stereo (#273). That is not a
// matter of processing both channels independently either: two independent
// limiters on a stereo pair pull different gains and the image shifts, so it
// wants a linked detector. Left as a single documented assumption rather than
// a half-built stereo path nobody can test.
func (p *PcmSpeaker) applyOutputChain(out []byte) []byte {
	p.oc.mu.Lock()
	chain := p.oc.chain
	p.oc.mu.Unlock()
	if chain == nil {
		return out
	}

	silent := isSilence(out)
	if silent {
		if p.oc.drain <= 0 {
			return out
		}
		p.oc.drain--
	} else {
		p.oc.drain = 1
	}

	frames := len(out) / 4 // 2 channels, 2 bytes each
	if cap(p.oc.mono) < frames {
		p.oc.mono = make([]float64, frames)
		p.oc.stereoOut = make([]byte, len(out))
	}
	mono := p.oc.mono[:frames]
	for i := 0; i < frames; i++ {
		mono[i] = float64(int16(binary.LittleEndian.Uint16(out[i*4:])))
	}

	processed := chain.Process(mono)

	// The chain is 1:1 in steady state — the limiter's latency is absorbed by
	// its tail, not by short reads. Anything else would reshape the period and
	// desynchronise the ALSA write, so it is a bug rather than a case to
	// handle: fall back to the unprocessed mix rather than pumping a period of
	// the wrong length.
	if len(processed) != frames {
		return out
	}

	buf := p.oc.stereoOut[:len(out)]
	for i, v := range processed {
		s := clampToInt16(v)
		u := uint16(s)
		binary.LittleEndian.PutUint16(buf[i*4:], u)
		binary.LittleEndian.PutUint16(buf[i*4+2:], u)
	}
	return buf
}

// isSilence reports whether a period is entirely zero. Cheaper than it looks —
// the common idle case exits on the first non-zero byte, and a genuinely
// silent period is what the mixer hands back as the shared silencePeriod.
func isSilence(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// clampToInt16 saturates rather than wrapping, for the reason the mixer does:
// a wrap turns a loud peak into a full-scale opposite-polarity one, far worse
// than clipping. The limiter should make this unreachable; it is the same
// backstop the reference keeps, and for the same reason.
func clampToInt16(v float64) int16 {
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	}
	return int16(v)
}
