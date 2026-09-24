package led

import (
	"os"
	"path/filepath"
	"testing"
)

// The mute LED is the ONLY indicator of mute on this device — applyMute
// deliberately stopped painting the ring — and unmuting is device-sovereign,
// so somebody who cannot read it has no way to tell whether pressing the
// button will help. A line that comes up inverted turns every state it
// reports into its opposite, silently (#339).

func TestAnInvertedLineIsNormalised(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active_low")
	if err := os.WriteFile(path, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	normalisePolarityAt(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "0" {
		t.Fatalf("active_low = %q, want %q — the LED reports the opposite of the truth", got, "0")
	}
}

func TestALineAlreadyCorrectIsNotWritten(t *testing.T) {
	// eMMC that cannot be replaced, and this runs on every start. A write
	// that changes nothing is a write that should not happen.
	path := filepath.Join(t.TempDir(), "active_low")
	if err := os.WriteFile(path, []byte("0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	normalisePolarityAt(path)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("a line already at 0 was rewritten")
	}
}

func TestAKernelWithoutTheAttributeIsNotAFault(t *testing.T) {
	// There is nothing to invert on a kernel that does not expose it, and an
	// unmuted boot without a button LED is cosmetic. Refusing to come up over
	// it is not.
	normalisePolarityAt(filepath.Join(t.TempDir(), "does-not-exist"))
}

func TestTheInitPathNormalisesBeforeItPaints(t *testing.T) {
	// Source guard: SetMuteButtonLED(false) writing through an inverted line
	// leaves the LED LIT on an unmuted boot, which is the reported symptom.
	// Comments are not stripped here on purpose — the call is a bare
	// identifier that appears nowhere else in the file.
	src, err := os.ReadFile("mute_button.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	norm := indexOf(text, "normaliseMuteButtonPolarity()\n\treturn SetMuteButtonLED(false)")
	if norm < 0 {
		t.Fatal("InitMuteButtonLED no longer normalises the polarity immediately before painting")
	}
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
