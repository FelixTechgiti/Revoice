// Package sndcloexec keeps this process's ALSA devices out of its children.
//
// # The fault
//
// Measured on hardware 2026-09-13: librespot and shairport-sync each held
// `fd 3 -> /dev/snd/pcmC0D23p` and `fd 13 -> /dev/snd/pcmC0D24c` — the same
// descriptor numbers on the same devices as the firmware itself. librespot is
// built `--no-default-features`, which drops every audio backend but the pipe
// and with it alsa-sys, so it has no code that can open a PCM. The
// descriptors can only have been inherited across the fork.
//
// The cause is the boundary rather than a mistake at any call site: the PCM is
// opened by tinyalsa's `pcm_open`, which is C's `open(fn, O_RDWR)`. Go sets
// O_CLOEXEC on everything it opens itself; C does not, and nothing in Go's
// exec closes a descriptor it did not create.
//
// # Why it matters even though nothing is currently broken by it
//
// device/CLAUDE.md already names the failure a second holder sets up, for a
// different reason: two things opening the speaker is the #80 case, a blocking
// open with no timeout and eighteen minutes of a stranded device. A leaked
// descriptor is a holder nothing accounts for — closing the speaker here does
// not release the substream while a child still has it, so the next open can
// find it busy from a direction `waitForFreePcm` cannot see. And the children
// that hold it are daemons by design.
//
// # Why this marks the descriptor rather than cleaning the exec
//
// There are around twenty exec sites in this firmware — tinymix, wpa_cli,
// iptables, `sh`, the two endpoints — and every one of them inherits. Fixing
// the exec means fixing all of them and every one added later. Marking the
// descriptor once, where it is opened, covers all of them and cannot be
// forgotten by a future caller, which is the only version of this fix that
// stays true.
package sndcloexec

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// procFDDir is where this process's open descriptors are listed. A variable
// so a test can point it somewhere it controls.
var procFDDir = "/proc/self/fd"

// SoundDevicePrefix is what marks a descriptor as one of the card's.
//
// The whole directory, not just the PCMs: the control node carries mixer
// state and has no more business in a child than the stream does.
const SoundDevicePrefix = "/dev/snd/"

// IsSoundDevice reports whether a /proc/self/fd link target names one.
//
// Deliberately strict about the prefix. A link target can be `socket:[451468]`
// or `pipe:[452089]` or a deleted file rendered as `/dev/snd/x (deleted)`, and
// only the first component is a path at all.
func IsSoundDevice(target string) bool {
	return strings.HasPrefix(target, SoundDevicePrefix) &&
		len(target) > len(SoundDevicePrefix)
}

// SetCloexec marks one descriptor so it does not survive an exec.
func SetCloexec(fd int) error {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL,
		uintptr(fd), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		return errno
	}
	if flags&syscall.FD_CLOEXEC != 0 {
		return nil
	}
	_, _, errno = syscall.Syscall(syscall.SYS_FCNTL,
		uintptr(fd), uintptr(syscall.F_SETFD), flags|syscall.FD_CLOEXEC)
	if errno != 0 {
		return errno
	}
	return nil
}

// MarkOpenSoundDevices marks every /dev/snd descriptor this process holds and
// reports how many it marked.
//
// Errors on individual descriptors are swallowed rather than returned, and
// that is the right trade here: the listing races this process's own file
// activity, so a descriptor can close between the readdir and the readlink,
// and a caller that treated that as a failure would log an alarm about
// nothing. A descriptor that genuinely cannot be marked leaks exactly as it
// does today — no worse than not calling this at all.
//
// Called after each PCM open rather than before each exec, for the reason in
// the package comment. Reopening is covered because a descriptor is inherited
// only at fork: a child already running cannot acquire a new one.
func MarkOpenSoundDevices() int {
	d, err := os.Open(procFDDir)
	if err != nil {
		return 0
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return 0
	}

	n := 0
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		target, err := os.Readlink(procFDDir + "/" + name)
		if err != nil || !IsSoundDevice(target) {
			continue
		}
		// The directory handle above is itself in this listing and is a
		// directory, never /dev/snd, so it is filtered by the check rather
		// than by a special case.
		if SetCloexec(fd) == nil {
			n++
		}
	}
	return n
}
