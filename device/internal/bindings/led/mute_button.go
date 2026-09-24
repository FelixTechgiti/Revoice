package led

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Mute-button LED — the discrete red LED under the mic-off button, separate
// from the 12-LED ring. The line is SoC GPIO bank 5 bit 7 = pin 87 = sysfs
// gpio444, ACTIVE-HIGH (1 = lit). Found by regmap-tracing stock FireOS on a
// live biscuit (2026-07-19): each mute press writes pinctrl DIR-set 0x54
// then DOUT-set 0x454 (on) / DOUT-clr 0x458 (off), bit 7, bank 5.
//
// Do not trust libled_hal.so here: its k_muteButtonGPIOAddress = 0x1BD (445)
// is off by one from the kernel's sysfs numbering — pin 88's pad is muxed to
// MSDC2_DAT1 and writes to gpio445 reach nothing (v2.9.4 and earlier drove
// it; the button never lit). Stock itself bypasses sysfs via the /dev/mtgpio
// ioctl, which is why the HAL constant never had to agree with gpiolib.
const (
	muteButtonGPIO      = "444"
	gpioExportPath      = "/sys/class/gpio/export"
	muteButtonDirPath   = "/sys/class/gpio/gpio" + muteButtonGPIO + "/direction"
	muteButtonValuePath = "/sys/class/gpio/gpio" + muteButtonGPIO + "/value"
	// gpiolib inverts every read and write on this line when set. It is a
	// property of the exported line, not of the board — see
	// InitMuteButtonLED.
	muteButtonActiveLowPath = "/sys/class/gpio/gpio" + muteButtonGPIO + "/active_low"
)

// InitMuteButtonLED exports the GPIO if needed, forces output direction,
// NORMALISES THE POLARITY, and switches the LED off (the process starts
// unmuted; a crash while muted must not leave a stale red button on restart).
//
// # Why polarity is written rather than trusted
//
// The package comment's ACTIVE-HIGH was measured by regmap-tracing stock
// FireOS on a live biscuit. What that measurement is about is the BOARD — the
// pad, the transistor, which way round the LED is wired — and it stands.
// What it says nothing about is `active_low`, which is a property of the
// exported sysfs line rather than of the hardware: gpiolib inverts every read
// and write when it is set, and nothing guarantees its value on an export.
//
// A line that comes up inverted turns every state this LED reports into its
// opposite, silently, and that is not a cosmetic fault here: `applyMute`
// deliberately stopped painting the ring, so this GPIO is the ONLY indicator
// of mute on the device, and unmuting is device-sovereign — the button is the
// only way back, and somebody who cannot read it has no way to tell whether
// pressing will help. Measured that way on 2026-09-23 (#339): three readings,
// both directions, lit meaning live.
//
// Writing 0 costs one syscall and removes the question. It is deliberately
// NOT a second polarity constant: two devices disagreeing about which way
// round a GPIO is would put a per-platform branch in the one place this
// package has kept platform-free, and the sign would then be wrong on
// whichever device nobody tested.
//
// # And it reports what it found
//
// The cause is inferred rather than measured — nothing has read `active_low`
// off a device — so the value BEFORE the write is logged. A fix shipped on a
// guess with no way to check the guess is how the same conversation happens
// twice: the line goes to /tmp/server.log, which the controller relays, so
// the next boot answers it from the field.
//
// Every step past the export is non-fatal. An unmuted boot without a button
// LED is cosmetic; refusing to come up over it is not.
func InitMuteButtonLED() error {
	if _, err := os.Stat(muteButtonValuePath); os.IsNotExist(err) {
		if err := os.WriteFile(gpioExportPath, []byte(muteButtonGPIO), 0644); err != nil {
			return fmt.Errorf("mute button LED: export gpio%s: %w", muteButtonGPIO, err)
		}
	}
	if err := os.WriteFile(muteButtonDirPath, []byte("out"), 0644); err != nil {
		return fmt.Errorf("mute button LED: set direction: %w", err)
	}
	normaliseMuteButtonPolarity()
	return SetMuteButtonLED(false)
}

// normaliseMuteButtonPolarity forces gpiolib's inversion off, and says what it
// was. Absent on kernels that do not expose the attribute, which is fine —
// there is nothing to invert there.
func normaliseMuteButtonPolarity() { normalisePolarityAt(muteButtonActiveLowPath) }

// normalisePolarityAt is the half that can be driven off-target. The device
// path is a const, and what is worth pinning is the DECISION — that a line
// already at 0 is left alone, that anything else is written and said, and
// that a kernel without the attribute is not treated as a fault.
func normalisePolarityAt(path string) {
	before, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Mute button LED: no active_low attribute (%v) — polarity left to the kernel", err)
		return
	}
	was := strings.TrimSpace(string(before))
	if was == "0" {
		return
	}
	// Said only when it is NOT the expected value, so a healthy device stays
	// quiet and a line in a log is evidence rather than noise.
	log.Printf("Mute button LED: active_low was %q — forcing 0 (#339)", was)
	if err := os.WriteFile(path, []byte("0"), 0644); err != nil {
		log.Printf("Mute button LED: could not clear active_low: %v", err)
	}
}

// SetMuteButtonLED switches the red LED under the mic-off button.
// Active-high (see package comment): 1 = on, 0 = off.
func SetMuteButtonLED(on bool) error {
	v := []byte("0")
	if on {
		v = []byte("1")
	}
	if err := os.WriteFile(muteButtonValuePath, v, 0644); err != nil {
		return fmt.Errorf("mute button LED: write value: %w", err)
	}
	return nil
}
