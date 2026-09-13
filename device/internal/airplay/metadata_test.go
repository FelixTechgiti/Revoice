package airplay

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// A real pvol item, in the shape shairport-sync writes.
func pvolItem(payload string) string {
	return fmt.Sprintf(
		`<item><type>73736e63</type><code>70766f6c</code><length>%d</length>`+
			`<data encoding="base64">%s</data></item>`,
		len(payload), base64.StdEncoding.EncodeToString([]byte(payload)))
}

func collect(t *testing.T, stream string) []float64 {
	t.Helper()
	var got []float64
	VolumeFromMetadata(strings.NewReader(stream), func(db float64) {
		got = append(got, db)
	})
	return got
}

func TestAVolumeChangeIsRead(t *testing.T) {
	got := collect(t, pvolItem("-15.000000,-30.000000,-30.000000,0.000000"))
	if len(got) != 1 || got[0] != -15 {
		t.Fatalf("got %v, want [-15]", got)
	}
}

// Only the FIRST field. The other three are shairport's own derived numbers
// about a range it knows nothing about — the stdout backend exposes no volume
// control, so its idea of our hardware is invented.
func TestOnlyTheAirPlayFieldIsRead(t *testing.T) {
	// Second field deliberately different, to catch reading the wrong one.
	got := collect(t, pvolItem("-7.500000,-99.000000,-30.000000,0.000000"))
	if len(got) != 1 || got[0] != -7.5 {
		t.Fatalf("got %v, want [-7.5] — the wrong field is being read", got)
	}
}

// Mute arrives through the same item and must survive parsing intact, so the
// caller can recognise the sentinel rather than seeing a very quiet volume.
func TestMuteArrivesAsTheSentinel(t *testing.T) {
	got := collect(t, pvolItem("-144.000000,-144.000000,-30.000000,0.000000"))
	if len(got) != 1 || got[0] != -144 {
		t.Fatalf("got %v, want [-144]", got)
	}
}

// The stream carries dozens of item types. Reading one means ignoring the
// rest silently — cover art alone would otherwise put a line in the log per
// track change, on a device whose log is 89% noise already.
func TestOtherItemsAreIgnoredSilently(t *testing.T) {
	// A session-begin with no payload, a track title (core/asal), a chunk of
	// "cover art", then the volume.
	stream := `<item><type>73736e63</type><code>70726772</code><length>0</length></item>` +
		`<item><type>636f7265</type><code>6173616c</code><length>5</length>` +
		`<data encoding="base64">` + base64.StdEncoding.EncodeToString([]byte("Album")) + `</data></item>` +
		`<item><type>73736e63</type><code>50494354</code><length>9</length>` +
		`<data encoding="base64">` + base64.StdEncoding.EncodeToString([]byte("not-a-png")) + `</data></item>` +
		pvolItem("-3.000000,-30.000000,-30.000000,0.000000")

	got := collect(t, stream)
	if len(got) != 1 || got[0] != -3 {
		t.Fatalf("got %v, want exactly [-3]", got)
	}
}

// The stream never ends and has no root element, which is why it is scanned
// rather than decoded. Items arrive back to back with no separator.
func TestBackToBackItemsAreAllRead(t *testing.T) {
	stream := pvolItem("-30.000000,-30.000000,-30.000000,0.000000") +
		pvolItem("-20.000000,-30.000000,-30.000000,0.000000") +
		pvolItem("0.000000,-30.000000,-30.000000,0.000000")

	got := collect(t, stream)
	want := []float64{-30, -20, 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// Newlines and whitespace between items are what a real pipe looks like.
func TestWhitespaceBetweenItemsIsTolerated(t *testing.T) {
	stream := "\n" + pvolItem("-9.000000,-30.000000,-30.000000,0.000000") +
		"\n\n" + pvolItem("-1.000000,-30.000000,-30.000000,0.000000") + "\n"
	if got := collect(t, stream); len(got) != 2 {
		t.Fatalf("got %v, want two volumes", got)
	}
}

// A truncated item at the end of the stream — the process was killed
// mid-write — must not produce a bogus volume or panic.
func TestATruncatedItemIsDropped(t *testing.T) {
	stream := pvolItem("-5.000000,-30.000000,-30.000000,0.000000") +
		`<item><type>73736e63</type><code>70766f6c</code><length>9</length><data enc`
	got := collect(t, stream)
	if len(got) != 1 || got[0] != -5 {
		t.Fatalf("got %v, want only the complete item [-5]", got)
	}
}

// Garbage in the payload is data, not a crash: this reads a pipe written by
// another program, and the only safe response to something unparseable is to
// ignore it and keep reading.
func TestUnparseablePayloadsAreIgnored(t *testing.T) {
	for _, payload := range []string{"", "not-a-number", ",,,", "abc,-30,0,0"} {
		if got := collect(t, pvolItem(payload)); len(got) != 0 {
			t.Fatalf("payload %q produced %v, want nothing", payload, got)
		}
	}
	// Malformed base64 and malformed hex, likewise.
	bad := `<item><type>zzzz</type><code>70766f6c</code><data encoding="base64">##</data></item>`
	if got := collect(t, bad); len(got) != 0 {
		t.Fatalf("malformed item produced %v", got)
	}
}

// The config only asks for metadata when something is listening. A block
// telling shairport-sync to write down a pipe nobody opens fills its 64KB
// buffer and then BLOCKS the process decoding the audio — a silent stall of
// the music, caused by a diagnostic feature nobody switched on.
func TestNoMetadataBlockWithoutAReader(t *testing.T) {
	if got := renderConfig(0, ""); strings.Contains(got, "metadata") {
		t.Fatalf("metadata asked for with no reader:\n%s", got)
	}
	got := renderConfig(0, "/data/local/etc/revoice/airplay-metadata")
	if !strings.Contains(got, `enabled = "yes"`) {
		t.Fatalf("metadata not enabled:\n%s", got)
	}
	if !strings.Contains(got, "/data/local/etc/revoice/airplay-metadata") {
		t.Fatalf("the pipe path is not in the config:\n%s", got)
	}
}

// Cover art is megabytes per track down a pipe whose only reader wants twenty
// bytes of volume, on a device sharing 512MB with Android. The default is
// already "no" — this asserts what we rely on, so a future default flip
// arrives as a failing test rather than as a memory problem with no cause.
func TestCoverArtIsRefusedExplicitly(t *testing.T) {
	got := renderConfig(0, "/tmp/p")
	if !strings.Contains(got, `include_cover_art = "no"`) {
		t.Fatalf("cover art not refused:\n%s", got)
	}
}

// The latency offset and the metadata block have to coexist: they are
// independent settings and one must not overwrite the other's section.
func TestLatencyAndMetadataCoexist(t *testing.T) {
	got := renderConfig(0.171, "/tmp/p")
	if !strings.Contains(got, "audio_backend_latency_offset_in_seconds") {
		t.Fatalf("the latency offset was lost:\n%s", got)
	}
	if !strings.Contains(got, "pipe_name") {
		t.Fatalf("the metadata block was lost:\n%s", got)
	}
}

// stubReader replaces the real reader for tests about the RECONCILER, which
// is bookkeeping and should not be doing filesystem work to be observed. It
// also records every start, so "exactly one reader" can be asserted directly
// rather than inferred from a pointer.
func stubReader(t *testing.T) *int32 {
	t.Helper()
	var starts int32
	old := startMetadataReader
	startMetadataReader = func(path string, stop <-chan struct{}, fn func(float64)) {
		atomic.AddInt32(&starts, 1)
		<-stop
	}
	t.Cleanup(func() { startMetadataReader = old })
	return &starts
}

// Turning volume control ON while the receiver runs is the dangerous
// direction, and the reader has to exist before the config that asks for
// metadata. A FIFO with no reader fills at 64KB and then BLOCKS the writer —
// which is the process decoding the audio, so the symptom is music stopping.
func TestTheReaderExistsBeforeTheConfigAsksForMetadata(t *testing.T) {
	stubReader(t)
	dir := t.TempDir()
	c := New(Options{
		Binary:       filepath.Join(dir, "no-such-binary"),
		ConfigPath:   filepath.Join(dir, "shairport.conf"),
		MetadataPipe: filepath.Join(dir, "meta"),
	}, nil, nil)
	t.Cleanup(func() { c.SetVolumeHandler(nil); c.syncMetadataReader() })

	// Off: no pipe asked for, no reader.
	if got := c.metadataPipe(); got != "" {
		t.Fatalf("metadata asked for with no handler: %q", got)
	}
	c.syncMetadataReader()
	c.mu.Lock()
	if c.metaStop != nil {
		t.Fatal("a reader is running for a handler that does not exist")
	}
	c.mu.Unlock()

	// On: both.
	c.SetVolumeHandler(func(float64) {})
	c.syncMetadataReader()
	c.mu.Lock()
	running := c.metaStop != nil
	c.mu.Unlock()
	if !running {
		t.Fatal("no reader for an installed handler — the pipe would fill and " +
			"block shairport-sync")
	}
	if got := c.metadataPipe(); got == "" {
		t.Fatal("the config would not ask for metadata")
	}

	// Off again: the reader must go, or it outlives what it reads for.
	c.SetVolumeHandler(nil)
	c.syncMetadataReader()
	c.mu.Lock()
	stillRunning := c.metaStop != nil
	c.mu.Unlock()
	if stillRunning {
		t.Fatal("the reader outlived its handler")
	}
}

// Reconciling twice must not start a second reader on the same pipe: two
// readers on one FIFO split the stream between them and each sees half an
// item.
func TestSyncingTwiceStartsOneReader(t *testing.T) {
	starts := stubReader(t)
	dir := t.TempDir()
	c := New(Options{
		Binary:       filepath.Join(dir, "nope"),
		MetadataPipe: filepath.Join(dir, "meta"),
		OnVolume:     func(float64) {},
	}, nil, nil)
	// Stopped before the temp directory goes, or the reader outlives the test.
	t.Cleanup(func() { c.SetVolumeHandler(nil); c.syncMetadataReader() })

	c.syncMetadataReader()
	c.mu.Lock()
	first := c.metaStop
	c.mu.Unlock()
	if first == nil {
		t.Fatal("no reader was started for an installed handler")
	}

	// The deterministic half: the reconciler's own state must not move, which
	// is what proves nothing was launched. Asserted on the CHANNEL rather
	// than on a counter, because the counter is incremented inside the
	// goroutine and reading it here would race the very thing under test —
	// which is how the first version of this failed.
	for i := 0; i < 5; i++ {
		c.syncMetadataReader()
		c.mu.Lock()
		again := c.metaStop
		c.mu.Unlock()
		if again != first {
			t.Fatalf("sync %d replaced the reader — two readers on one FIFO "+
				"split the stream and each sees half an item", i+2)
		}
	}

	// And exactly one ever ran. Waited for rather than sampled: the start is
	// asynchronous by construction.
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(starts) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := atomic.LoadInt32(starts); got != 1 {
		t.Fatalf("%d readers started, want exactly 1", got)
	}
}

// THE regression, and the only test that would have caught it.
//
// shairport-sync does not wait for a reader. The first implementation held the
// read end for a few microseconds out of every second — open, EOF, close,
// sleep — so a writer that does not park simply never found one. Measured on
// hardware 2026-09-13: a live AirPlay session with audio playing, nothing
// holding the FIFO at either end, and not one pvol ever delivered.
//
// Every earlier test wrote with a plain os.OpenFile or a shell redirect, which
// BLOCKS until a reader appears — so our own tests were the one writer whose
// timing did not matter, and they all passed.
//
// So this one writes the way shairport does: non-blocking, no waiting, no
// retry. If the reader is not already holding the pipe open, O_WRONLY|O_NONBLOCK
// fails with ENXIO and nothing is delivered.
func TestANonBlockingWriterReachesTheReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "airplay-metadata")

	got := make(chan float64, 4)
	stop := make(chan struct{})
	defer close(stop)
	go readMetadataPipe(path, stop, func(db float64) { got <- db })

	// Give the reader a moment to create the FIFO and open it. It should then
	// hold it for good — that is the property under test.
	var w *os.File
	var err error
	for i := 0; i < 200; i++ {
		w, err = os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("a non-blocking writer could not open the pipe: %v — the "+
			"reader is not holding the read end, so shairport-sync's metadata "+
			"goes nowhere", err)
	}
	defer w.Close()

	if _, err := w.WriteString(pvolItem("-20.500000,-30.000000,-30.000000,0.000000")); err != nil {
		t.Fatalf("writing to the pipe failed: %v", err)
	}

	select {
	case db := <-got:
		if db != -20.5 {
			t.Errorf("delivered %v dB, want -20.5", db)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the item was written but never arrived")
	}
}

// And the reader must STAY available: shairport writes metadata repeatedly
// across a session, and a reader that let go after the first item would work
// exactly once — which is indistinguishable from working, in a manual test.
func TestTheReaderStaysAvailableAcrossSeveralWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "airplay-metadata")

	got := make(chan float64, 8)
	stop := make(chan struct{})
	defer close(stop)
	go readMetadataPipe(path, stop, func(db float64) { got <- db })

	var w *os.File
	var err error
	for i := 0; i < 200; i++ {
		if w, err = os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("non-blocking open failed: %v", err)
	}
	defer w.Close()

	for _, want := range []float64{-30, -20, -10} {
		payload := fmt.Sprintf("%f,-30.000000,-30.000000,0.000000", want)
		if _, err := w.WriteString(pvolItem(payload)); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		select {
		case db := <-got:
			if db != want {
				t.Errorf("delivered %v dB, want %v", db, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%v dB never arrived — the reader let go after an earlier item", want)
		}
	}
}
