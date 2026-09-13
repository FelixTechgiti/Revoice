package airplay

import (
	"log"
	"os"
	"syscall"
	"time"
)

// Reading shairport-sync's metadata off a FIFO, which has one trap that
// matters and one that would be worse.
//
// # Opening a FIFO for reading BLOCKS until a writer appears
//
// That is POSIX, not a shairport quirk: `open(fifo, O_RDONLY)` parks until
// somebody opens the write end. Doing it on the goroutine that starts the
// endpoint would hang the whole start path until a phone connected — which
// is to say, until after the thing that needed starting had started.
//
// O_NONBLOCK on the open fixes exactly that and nothing else: it returns
// immediately with no writer, and reads then return EOF rather than
// blocking. So the loop below reopens rather than spinning on a dead file
// descriptor.
//
// # A pipe with no reader eventually blocks the WRITER
//
// The worse trap, and it is why this exists at all rather than a config
// block on its own. A FIFO holds 64KB by default; once full, shairport-sync
// blocks trying to write metadata — and the process blocked is the one
// decoding audio. So the pipe is created and drained by us, or it is not
// asked for: `metadataPipe()` returns "" when nothing is listening, and no
// metadata block is written at all.
//
// # Why the read end is held O_RDWR, and why O_RDONLY made the feature dead
//
// **The reasoning above is right and the first implementation of it was not.**
// It opened O_RDONLY|O_NONBLOCK, read to EOF — immediate, with no writer
// attached — closed, slept a second, and reopened. So the read end existed for
// a few microseconds out of every second, and a writer that does not WAIT for
// a reader simply never found one.
//
// Measured on hardware 2026-09-13, during a live AirPlay session with audio
// playing: nothing held the FIFO at either end, and not one `pvol` had ever
// arrived. A synthetic item written by hand went through immediately — because
// a shell redirect is a BLOCKING writer and parks until the reader shows up.
// That difference is the whole bug: our own test was the one writer whose
// timing did not matter.
//
// Holding the read end O_RDWR means the kernel always sees a reader, so the
// question of whether shairport-sync blocks, polls or gives up never arises —
// it cannot miss us. It also removes the EOF the reopen loop existed to
// handle, since a pipe with a live write end never reports one.
//
// `internal/spotify`'s reader has always done this, and its comment says why
// in one line: "One flag removes both problems." The comment HERE claimed the
// AirPlay case was different because "shairport-sync opens the write end
// itself and holds it" — an assumption about somebody else's process, written
// as a fact, and the measurement says it does not hold it.

// startMetadataReader is the reader syncMetadataReader launches, injectable
// so the RECONCILER can be tested without doing filesystem work.
//
// It exists because the first version of those tests started the real reader,
// which creates a FIFO — and then raced `t.TempDir()`'s cleanup, which listed
// the directory as empty, tried to remove it, and found the pipe had appeared
// in between. It failed as `directory not empty`, on main, in a test whose
// subject is a boolean.
//
// The lesson is the general one: a test about bookkeeping should not be doing
// I/O to find out what the bookkeeping says.
var startMetadataReader = readMetadataPipe

// pipeRetryDelay is how long to wait before trying again when the FIFO could
// not be opened at all. Long, because a failure to open a path we just created
// is a fault of the filesystem rather than of this moment, and the receiver
// works without metadata.
const pipeRetryDelay = 30 * time.Second

// readMetadataPipe creates the FIFO if needed and reads it for the life of
// the stop channel, reporting AirPlay volume changes.
//
// Every failure is logged once and retried, never fatal. This is a
// convenience on top of a receiver that works without it: an AirPlay endpoint
// that refused to start because a pipe could not be made would be a strictly
// worse device than one whose volume slider does nothing.
func readMetadataPipe(path string, stop <-chan struct{}, onVolume func(db float64)) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		if err := ensureFIFO(path); err != nil {
			log.Printf("[airplay] no metadata pipe at %s: %v — the AirPlay "+
				"volume slider will not move this device", path, err)
			if !sleepOrStop(stop, pipeRetryDelay) {
				return
			}
			continue
		}

		// O_RDWR, for the reason in the package comment: it makes the kernel
		// see a reader for as long as this runs, so shairport-sync cannot
		// write into a pipe nobody is on. It also never blocks on open and
		// never reports EOF, which is why there is no reopen loop below.
		f, err := os.OpenFile(path, os.O_RDWR, os.ModeNamedPipe)
		if err != nil {
			log.Printf("[airplay] cannot open metadata pipe: %v", err)
			if !sleepOrStop(stop, pipeRetryDelay) {
				return
			}
			continue
		}

		// Closing the file is what unblocks the read, so the watcher has to
		// own that rather than the reader noticing a flag — the same shape as
		// internal/spotify's event reader.
		done := make(chan struct{})
		go func() {
			select {
			case <-stop:
				f.Close()
			case <-done:
			}
		}()

		VolumeFromMetadata(f, onVolume)
		close(done)
		f.Close()

		// Returning at all means the scan stopped — a malformed stream, or the
		// file closed under us on the way out. Neither is expected, so it is
		// worth a line, and the retry is slow for the same reason.
		select {
		case <-stop:
			return
		default:
		}
		log.Printf("[airplay] metadata reader returned unexpectedly — retrying "+
			"in %s; the volume slider will not move this device until it does",
			pipeRetryDelay)
		if !sleepOrStop(stop, pipeRetryDelay) {
			return
		}
	}
}

// ensureFIFO makes the path a named pipe, or reports why it cannot be.
//
// An existing FIFO is left alone: recreating it would break a writer already
// attached to it, which after a firmware restart is exactly the shairport-sync
// still running from before. Anything else at that path is replaced — a
// regular file there would accept writes for ever and read back as an
// ever-growing log nobody drains.
func ensureFIFO(path string) error {
	if st, err := os.Stat(path); err == nil {
		if st.Mode()&os.ModeNamedPipe != 0 {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return syscall.Mkfifo(path, 0o600)
}

// sleepOrStop waits, and reports false if it was told to stop instead.
func sleepOrStop(stop <-chan struct{}, d time.Duration) bool {
	select {
	case <-stop:
		return false
	case <-time.After(d):
		return true
	}
}
