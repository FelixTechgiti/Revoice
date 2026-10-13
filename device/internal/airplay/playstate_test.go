package airplay

import (
	"encoding/hex"
	"strings"
	"testing"
)

// metaItem builds one item the way shairport-sync writes it: type and code as
// four ASCII bytes in hex. Written here rather than reused from the volume
// tests so a change to those cannot silently change what this asserts.
func metaItem(typ, code string) string {
	return "<item><type>" + hex.EncodeToString([]byte(typ)) +
		"</type><code>" + hex.EncodeToString([]byte(code)) +
		"</code><length>0</length></item>"
}

func statesFrom(t *testing.T, codes ...string) []PlayState {
	t.Helper()
	var b strings.Builder
	for _, c := range codes {
		b.WriteString(metaItem("ssnc", c))
	}
	var got []PlayState
	WatchMetadata(strings.NewReader(b.String()), nil, func(s PlayState) {
		got = append(got, s)
	})
	return got
}

// The whole of #387: a pause has to arrive, because the music plane is only
// emptied by somebody telling it to.
func TestPauseAndStopAreReported(t *testing.T) {
	got := statesFrom(t, "paus", "pend")
	want := []PlayState{PlayStatePaused, PlayStateStopped}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// shairport says "playing" with two different codes and neither may be lost:
// `pbeg` opens a session, `pres` resumes one, and a resume that is not
// reported leaves whatever acted on the pause believing the sender is still
// stopped.
func TestBothPlayingCodesAreReported(t *testing.T) {
	for _, code := range []string{"pbeg", "pres"} {
		got := statesFrom(t, code)
		if len(got) != 1 || got[0] != PlayStatePlaying {
			t.Fatalf("%s: got %v, want [playing]", code, got)
		}
	}
}

// Everything else on this stream is somebody else's business — cover art
// alone would otherwise put a callback in per track change.
func TestUnrelatedItemsReportNothing(t *testing.T) {
	if got := statesFrom(t, "PICT", "mden", "pfls", "prsm"); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}

// The two halves ride one stream, so a volume item must not be read as a
// transport change nor the other way round.
func TestVolumeAndTransportDoNotCrossOver(t *testing.T) {
	vol := "<item><type>73736e63</type><code>70766f6c</code><length>25</length>" +
		"<data encoding=\"base64\">LTE1LjAwMDAwMCwtMzAuMDAwMDAwLC0zMC4wMDAwMDAsMC4wMDAwMDA=</data></item>"
	stream := vol + metaItem("ssnc", "paus")

	var dbs []float64
	var states []PlayState
	WatchMetadata(strings.NewReader(stream),
		func(db float64) { dbs = append(dbs, db) },
		func(s PlayState) { states = append(states, s) })

	if len(dbs) != 1 || dbs[0] != -15 {
		t.Fatalf("volume: got %v, want [-15]", dbs)
	}
	if len(states) != 1 || states[0] != PlayStatePaused {
		t.Fatalf("transport: got %v, want [paused]", states)
	}
}

// A nil handler is an ordinary configuration, not a reason to panic: the
// volume half is a user setting and may genuinely be absent.
func TestEitherHandlerMayBeNil(t *testing.T) {
	stream := metaItem("ssnc", "paus") +
		"<item><type>73736e63</type><code>70766f6c</code><length>25</length>" +
		"<data encoding=\"base64\">LTE1LjAwMDAwMCwtMzAuMDAwMDAwLC0zMC4wMDAwMDAsMC4wMDAwMDA=</data></item>"
	WatchMetadata(strings.NewReader(stream), nil, nil)
}

// The gate decides whether shairport is asked for metadata at all. Before
// #387 it was the volume handler alone — which defaults off, so the pause
// could never be seen on an ordinary device.
func TestATransportHandlerAloneAsksForMetadata(t *testing.T) {
	c := &Client{opts: Options{
		OnPlayState:  func(PlayState) {},
		MetadataPipe: "/tmp/does-not-need-to-exist",
	}}
	if got := c.metadataPipe(); got == "" {
		t.Fatal("a transport handler alone must ask for metadata")
	}
}

func TestNeitherHandlerAsksForNothing(t *testing.T) {
	c := &Client{opts: Options{MetadataPipe: "/tmp/does-not-need-to-exist"}}
	if got := c.metadataPipe(); got != "" {
		t.Fatalf("metadataPipe = %q, want empty with no handler", got)
	}
}

// A receiver must not be killed mid-track to rewrite a config file that is
// about to be identical. The restart follows the metadata GATE, so what has
// to be true is that the gate does not move when the volume setting does —
// and that it still moves when the volume handler is the only one there.
func TestTheVolumeSettingMovesTheGateOnlyWhenItIsTheOnlyHandler(t *testing.T) {
	withTransport := &Client{opts: Options{
		OnPlayState:  func(PlayState) {},
		MetadataPipe: "/tmp/does-not-need-to-exist",
	}}
	before := withTransport.metadataPipe() != ""
	withTransport.opts.OnVolume = func(float64) {}
	after := withTransport.metadataPipe() != ""
	if !before || !after {
		t.Fatalf("with a transport handler the gate must stay open: %v -> %v",
			before, after)
	}

	alone := &Client{opts: Options{MetadataPipe: "/tmp/does-not-need-to-exist"}}
	if alone.metadataPipe() != "" {
		t.Fatal("no handler at all must leave the gate shut")
	}
	alone.opts.OnVolume = func(float64) {}
	if alone.metadataPipe() == "" {
		t.Fatal("the volume handler alone must still open the gate")
	}
}
