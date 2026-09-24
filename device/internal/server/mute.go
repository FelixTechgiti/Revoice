package server

import (
	"log"
	"sync"

	internalLed "github.com/wilbowes/EchoMuse/internal/bindings/led"
	"github.com/wilbowes/EchoMuse/internal/bindings/mixer"
	"github.com/wilbowes/EchoMuse/pkg/led"
)

type muteController struct {
	mu      sync.Mutex
	muted   bool
	ledCtrl func() led.Controller
	// dotMuted is set externally to block dot button events while muted
	onMuteChange func(muted bool)
	// persist, when set, is called after every Toggle() so the mute state
	// survives reboots and OTA restarts (state.json). Separate from
	// onMuteChange: that one is the controller-notification hook wired by
	// cmd, this one is internal.
	persist func()
}

func newMuteController(ledGetter func() led.Controller, onMuteChange func(muted bool)) *muteController {
	return &muteController{
		ledCtrl:      ledGetter,
		onMuteChange: onMuteChange,
	}
}

// SetOnMuteChange wires a callback invoked when mute state changes.
// B7 fix (2026-07-05 review): previously Server.SetMuteChangeCallback
// reached directly into m.mu/m.onMuteChange from outside this struct.
// Encapsulating the lock here keeps muteController responsible for its
// own synchronisation, matching every other muteController method.
func (m *muteController) SetOnMuteChange(cb func(muted bool)) {
	m.mu.Lock()
	m.onMuteChange = cb
	m.mu.Unlock()
}

func (m *muteController) IsMuted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.muted
}

func (m *muteController) Toggle() {
	m.mu.Lock()
	m.muted = !m.muted
	muted := m.muted
	// Copy under the lock — SetOnMuteChange writes this field under mu from
	// the main goroutine, and button events can fire before that wiring
	// completes (SubscribeToButton starts the evdev goroutines first).
	cb := m.onMuteChange
	persist := m.persist
	m.mu.Unlock()

	if muted {
		m.applyMute()
	} else {
		m.applyUnmute()
	}
	if persist != nil {
		persist()
	}

	if cb != nil {
		cb(muted)
	}
}

// MuteOnly mutes, and can only mute. Idempotent: muting an already-muted
// device does nothing at all rather than toggling it back.
//
// This is the entry point for a mute arriving over the network (Home
// Assistant). Toggle() stays the button's, and remains the ONLY way back to
// a live microphone. See the mute_set case in internal/client/control.go for
// why unmuting is a physical act.
func (m *muteController) MuteOnly() {
	m.mu.Lock()
	if m.muted {
		m.mu.Unlock()
		return
	}
	m.muted = true
	cb := m.onMuteChange
	persist := m.persist
	m.mu.Unlock()

	m.applyMute()
	if persist != nil {
		persist()
	}
	if cb != nil {
		cb(true)
	}
}

// adcMuteCtls are the per-chip ADC mute controls, all four codecs
// (A: ch0/ch1 … D: ch6 + unused). C5 hardware fix (2026-07-07): only chip
// A was muted before, leaving chips B–D — including ch6, the mic wake word
// and STT actually use — physically hot; the mic stream-stop was what made
// mute effective. By name since 2026-09-17 (#546).
var adcMuteCtls = []string{
	"ADC_A Left Mute", "ADC_A Right Mute",
	"ADC_B Left Mute", "ADC_B Right Mute",
	"ADC_C Left Mute", "ADC_C Right Mute",
	"ADC_D Left Mute", "ADC_D Right Mute",
}

// setAdcMute reports every failure, not just the first per control: this is
// the hardware half of the mute, and a silent miss here is a hot microphone.
func setAdcMute(val string) {
	failed := 0
	for _, ctl := range adcMuteCtls {
		if mixer.Set(ctl, val) != nil {
			failed++
		}
	}
	if failed > 0 {
		log.Printf("Mute: %d of %d ADC mute controls failed to set %s", failed, len(adcMuteCtls), val)
	}
	reportAdcMute(val)
}

// reportAdcMute reads one control back and says what it holds.
//
// A write that the mixer ACCEPTS is not a write that did what we meant, and
// that gap is the whole of #339: the LED and the microphone both behave as the
// opposite of `m.muted`, the mixer reports no failures, and nothing in this
// firmware has ever said what the control actually reads afterwards. The same
// shape as #546, where addressing a control by number wrote a perfectly valid
// value to the wrong one and failed silently on every FireOS 6 device.
//
// One control, not eight: they are written together and a disagreement between
// them is a different fault from the one being measured, so eight lines per
// toggle would be noise. It is logged unconditionally — these are rare events,
// a handful a day at most, and a line that only appears when somebody already
// suspects something is a line nobody has when they need it.
func reportAdcMute(val string) {
	got, err := mixer.Get(adcMuteCtls[0])
	if err != nil {
		log.Printf("Mute: could not read %s back: %v", adcMuteCtls[0], err)
		return
	}
	log.Printf("Mute: %s reads %q after writing %q", adcMuteCtls[0], got, val)
}

// RestoreMuted re-applies a persisted muted state at boot: flag + ADC mute
// only. The LED hardware isn't up yet when this runs (NewServer, before the
// LED-init goroutine finishes), so the red ring and button LED are painted
// by that goroutine once the controllers exist.
func (m *muteController) RestoreMuted() {
	m.mu.Lock()
	m.muted = true
	m.mu.Unlock()
	log.Println("Mute: restoring persisted muted state")
	setAdcMute("1")
}

// applyMute / applyUnmute deliberately do NOT touch the ring.
//
// Mute used to paint all twelve LEDs red and suppress every other paint,
// which made the ring the mute indicator and left it unavailable for
// anything else. The button's own LED has reported mute since v2.9.5 —
// stock parity, and the comment on setMuteButtonLED already said "the
// button itself shows muted, NOT JUST THE RING" — so the ring was the
// second copy of a signal that has dedicated hardware.
//
// Giving it up buys a ring that Home Assistant can own completely. What it
// costs is that a glance at the ring no longer tells you the mic is off;
// the red button LED does, and it is the one indicator that cannot be
// overpainted, because it is a GPIO rather than part of the ring driver.
// That is a deliberate trade, asked for and made with the cost stated.
func (m *muteController) applyMute() {
	log.Println("Mute: mic muted")
	setAdcMute("1")
	setMuteButtonLED(true)
}

func (m *muteController) applyUnmute() {
	log.Println("Mute: mic unmuted")
	setAdcMute("0")
	setMuteButtonLED(false)
}

// setMuteButtonLED drives the discrete red LED under the mic-off button —
// stock-Alexa parity: the button itself shows muted, not just the ring.
// GPIO-backed and independent of the ring driver, so it needs no repaint
// protection (ring repaints can't stomp it) and survives every LED-mode
// transition for free. Direct binding call, same precedent as setAdcMute's
// tinymix exec above.
func setMuteButtonLED(on bool) {
	if err := internalLed.SetMuteButtonLED(on); err != nil {
		log.Printf("Mute button LED: %v", err)
	}
}
