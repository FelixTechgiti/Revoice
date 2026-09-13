package spotify

import (
	"strings"
	"testing"
)

// The lines below are verbatim from /tmp/server.log on G090L91180250AN1,
// 2026-09-13 12:58:11, with our own `[librespot] ` relay prefix removed —
// which is what relayLog sees. The dump is abridged in the middle; nothing
// that the filter looks at was cut.
var djDump = []string{
	`[2026-09-13T12:58:11Z ERROR librespot_connect::state::context] context didn't have any tracks: Context {`,
	`        uri: Some(`,
	`            "spotify:playlist:37i9dQZF1EYkqdzj48dyYq",`,
	`        ),`,
	`        url: Some(`,
	`            "context://spotify:playlist:37i9dQZF1EYkqdzj48dyYq",`,
	`        ),`,
	`        metadata: {`,
	`            "context_owner": "spotify",`,
	`            "lexicon_context_url": "hm://lexicon-session-provider/context-resolve/v2/session?contextUri=spotify:playlist:37i9dQZF1EYkqdzj48dyYq",`,
	`            "context_description": "DJ",`,
	`        },`,
	`        pages: [`,
	`            ContextPage {`,
	`                page_url: None,`,
	`                next_page_url: None,`,
	`                tracks: [],`,
	`            },`,
	`        ],`,
	`    }`,
}

const djError = `[2026-09-13T12:58:11Z ERROR librespot_connect::spirc] Invalid state { the provided context has no tracks }`

// run feeds lines through a fresh filter and returns everything it emitted.
func run(lines ...string) []relayed {
	var f logFilter
	var out []relayed
	for _, l := range lines {
		out = append(out, f.Line(l)...)
	}
	return append(out, f.Flush()...)
}

func texts(out []relayed) []string {
	s := make([]string, len(out))
	for i, r := range out {
		s[i] = r.text
	}
	return s
}

func TestTheDumpIsReplacedByOneLineThatSaysWhatWasInIt(t *testing.T) {
	out := run(append(append([]string{}, djDump...), djError)...)

	// Opening line, summary, the error, the note. Nothing else.
	if len(out) != 4 {
		t.Fatalf("want 4 lines, got %d:\n%s", len(out), strings.Join(texts(out), "\n"))
	}
	if out[0].text != djDump[0] {
		t.Errorf("the line that opens the dump must still be relayed, got %q", out[0].text)
	}

	sum := out[1].text
	for _, want := range []string{
		"19 further lines",                        // len(djDump)-1
		"spotify:playlist:37i9dQZF1EYkqdzj48dyYq", // what it was
		"(DJ)",             // what the user called it
		"session provider", // why it is empty
	} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary is missing %q: %s", want, sum)
		}
	}
	if out[1].tag != "spotify" {
		t.Errorf("the summary is ours, not librespot's, got tag %q", out[1].tag)
	}

	if out[2].text != djError {
		t.Errorf("the error itself must be relayed verbatim, got %q", out[2].text)
	}
	if out[3].tag != "spotify" || !strings.Contains(out[3].text, "cannot play through Connect") {
		t.Errorf("want our note after the error, got %+v", out[3])
	}
	// The half that stops somebody restarting a healthy endpoint.
	if !strings.Contains(out[3].text, "unaffected") {
		t.Errorf("the note must say what still works: %s", out[3].text)
	}
}

// The point of keying on the metadata rather than on the word: a context that
// is simply empty is summarised, but nothing is claimed about why.
func TestAnEmptyContextWithNoSessionProviderGetsNoExplanation(t *testing.T) {
	var dump []string
	for _, l := range djDump {
		if strings.Contains(l, lexiconKey) || strings.Contains(l, "context_description") {
			continue
		}
		dump = append(dump, l)
	}
	out := run(append(dump, djError)...)

	for _, r := range out {
		if strings.Contains(r.text, "cannot play through Connect") {
			t.Fatalf("explained a context we know nothing about: %s", r.text)
		}
		if strings.Contains(r.text, "session provider") {
			t.Fatalf("claimed a session provider that was not in the dump: %s", r.text)
		}
	}
	if !strings.Contains(out[1].text, "further lines") {
		t.Errorf("the dump is still collapsed: %s", out[1].text)
	}
}

func TestTheSameFailureIsExplainedOnceNotByEveryLayerThatReportsIt(t *testing.T) {
	lines := append(append([]string{}, djDump...), djError,
		`[2026-09-13T12:58:11Z ERROR librespot_connect::context_resolver] setup of state failed: Invalid state { the provided context has no tracks }`)
	out := run(lines...)

	n := 0
	for _, r := range out {
		if strings.Contains(r.text, "cannot play through Connect") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want the note once, got %d times", n)
	}
}

func TestOrdinaryLinesGoThroughUntouched(t *testing.T) {
	lines := []string{
		`[2026-09-13T12:56:26Z INFO  librespot_playback::player] Loading <BETTER DAYZ> with Spotify URI <spotify:track:588ucNHZX2fxyj4gNq3RHF>`,
		`[2026-09-13T12:58:13Z INFO  librespot_connect::spirc] device became inactive`,
	}
	out := run(lines...)
	if len(out) != len(lines) {
		t.Fatalf("want %d lines, got %d: %s", len(lines), len(out), strings.Join(texts(out), " | "))
	}
	for i, r := range out {
		if r.text != lines[i] || r.tag != "librespot" {
			t.Errorf("line %d altered: %+v", i, r)
		}
	}
}

// A crash must never be the thing that gets swallowed. Its first line is not
// indented, so it is not dump body, and it does not open a dump either.
func TestAPanicIsNotSwallowed(t *testing.T) {
	out := run(`thread 'main' panicked at connect/src/spirc.rs:1:1:`)
	if len(out) != 1 || !strings.Contains(out[0].text, "panicked") {
		t.Fatalf("a panic must be relayed: %+v", out)
	}
}

func TestADumpCutOffByTheProcessExitingStillReportsItself(t *testing.T) {
	out := run(djDump...)
	last := out[len(out)-1]
	if !strings.Contains(last.text, "further lines") {
		t.Fatalf("a truncated dump must still be summarised, got %q", last.text)
	}
	if !strings.Contains(last.text, "spotify:playlist:37i9dQZF1EYkqdzj48dyYq") {
		t.Errorf("and must still say which context it was: %s", last.text)
	}
}

func TestTheNoteNamesTheMechanismRatherThanTheOneContextItWasFoundOn(t *testing.T) {
	note := sessionProviderNote("DJ")
	// Keying on the display string would make this break the day Spotify
	// localises it; keying on the playlist id would make it break the day
	// they mint another one.
	if strings.Contains(note, "37i9dQZF1EYkqdzj48dyYq") {
		t.Error("the note must not hardcode the playlist it was measured on")
	}
	if !strings.Contains(note, lexiconKey) {
		t.Error("the note must name the metadata field that identifies the condition")
	}
}
