package sndcloexec

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOnlySoundDeviceTargetsCount(t *testing.T) {
	// Every one of these is a real /proc/self/fd link target read off the
	// device on 2026-09-13, including the two that started this.
	yes := []string{
		"/dev/snd/pcmC0D23p",
		"/dev/snd/pcmC0D24c",
		"/dev/snd/controlC0",
	}
	no := []string{
		"/dev/null",
		"/dev/__properties__",
		"/data/local/etc/revoice/airplay-metadata",
		"socket:[451468]",
		"pipe:[452089]",
		"/proc/self/fd",
		// The prefix alone is the directory, not a device, and marking a
		// directory handle would be a silent no-op that inflates the count.
		"/dev/snd/",
		// A path that merely contains the prefix is not one: the check is an
		// anchor, not a substring.
		"/data/local/dev/snd/pcmC0D23p",
	}
	for _, s := range yes {
		if !IsSoundDevice(s) {
			t.Errorf("%q should count", s)
		}
	}
	for _, s := range no {
		if IsSoundDevice(s) {
			t.Errorf("%q should NOT count", s)
		}
	}
}

func TestSetCloexecActuallySetsTheFlag(t *testing.T) {
	// A plain file, opened the way C would: os.Open sets O_CLOEXEC itself, so
	// the flag is cleared first to reproduce what tinyalsa's open leaves
	// behind. Otherwise this test passes against a no-op.
	f, err := os.Create(filepath.Join(t.TempDir(), "x"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd := int(f.Fd())

	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL,
		uintptr(fd), uintptr(syscall.F_SETFD), 0); errno != 0 {
		t.Fatalf("could not clear the flag to set up: %v", errno)
	}
	if got := cloexec(t, fd); got {
		t.Fatal("setup failed — the flag is still set")
	}

	if err := SetCloexec(fd); err != nil {
		t.Fatalf("SetCloexec: %v", err)
	}
	if !cloexec(t, fd) {
		t.Error("the descriptor would still cross an exec")
	}
}

func TestSetCloexecIsIdempotent(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "x"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := SetCloexec(int(f.Fd())); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := SetCloexec(int(f.Fd())); err != nil {
		t.Fatalf("second: %v", err)
	}
}

func TestSetCloexecReportsAClosedDescriptor(t *testing.T) {
	if err := SetCloexec(-1); err == nil {
		t.Error("want an error for a descriptor that is not open")
	}
}

// MarkOpenSoundDevices walks a directory of symlinks, so a directory of
// symlinks is what it can be driven with — no /dev/snd needed, and the host
// running this has none.
func TestItMarksTheSoundDescriptorsAndLeavesTheRestAlone(t *testing.T) {
	dir := t.TempDir()
	snd, err := os.Create(filepath.Join(dir, "target-snd"))
	if err != nil {
		t.Fatal(err)
	}
	defer snd.Close()
	other, err := os.Create(filepath.Join(dir, "target-other"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	clear := func(fd int) {
		if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL,
			uintptr(fd), uintptr(syscall.F_SETFD), 0); errno != 0 {
			t.Fatalf("clear: %v", errno)
		}
	}
	clear(int(snd.Fd()))
	clear(int(other.Fd()))

	fdDir := filepath.Join(dir, "fd")
	if err := os.Mkdir(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The link TARGETS are what the function reads; they need not exist.
	link := func(fd int, target string) {
		if err := os.Symlink(target, filepath.Join(fdDir, itoa(fd))); err != nil {
			t.Fatal(err)
		}
	}
	link(int(snd.Fd()), "/dev/snd/pcmC0D23p")
	link(int(other.Fd()), "/data/local/etc/revoice/airplay-metadata")
	// A name that is not a number at all, which /proc never produces and a
	// careless parse would turn into fd 0.
	if err := os.Symlink("/dev/snd/pcmC0D24c", filepath.Join(fdDir, "notanumber")); err != nil {
		t.Fatal(err)
	}

	old := procFDDir
	procFDDir = fdDir
	defer func() { procFDDir = old }()

	if n := MarkOpenSoundDevices(); n != 1 {
		t.Errorf("marked %d descriptors, want 1", n)
	}
	if !cloexec(t, int(snd.Fd())) {
		t.Error("the sound descriptor was not marked")
	}
	if cloexec(t, int(other.Fd())) {
		t.Error("marked a descriptor that is not the card's")
	}
}

func TestAnUnreadableListingIsNotAFailure(t *testing.T) {
	old := procFDDir
	procFDDir = filepath.Join(t.TempDir(), "does-not-exist")
	defer func() { procFDDir = old }()

	// Silence rather than a panic or an error: this runs on the startup path
	// of a speaker, and a device that refused to play because it could not
	// read /proc would be strictly worse than one that leaks a descriptor.
	if n := MarkOpenSoundDevices(); n != 0 {
		t.Errorf("marked %d, want 0", n)
	}
}

func cloexec(t *testing.T, fd int) bool {
	t.Helper()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL,
		uintptr(fd), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		t.Fatalf("F_GETFD: %v", errno)
	}
	return flags&syscall.FD_CLOEXEC != 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
