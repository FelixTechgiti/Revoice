package spotify

import (
	"fmt"
	"strings"
)

// Filtering librespot's stderr, because one class of error prints sixty lines
// of protobuf and the one line that matters is not among them.
//
// librespot rejects a play context it cannot use like this
// (connect/src/state/context.rs, v0.8.0):
//
//	if context.pages.iter().all(|p| p.tracks.is_empty()) {
//	    error!("context didn't have any tracks: {context:#?}");
//
// `{:#?}` is Rust's pretty debug format, so the whole Context protobuf lands
// in the log one field per line — ~60 lines for a Spotify DJ context, every
// time the user selects it, and they are not diagnostic: the same three
// facts repeat inside a wall of `special_fields` and `cached_size`.
//
// So the dump is collapsed to one line carrying what identifies it — the
// context URI and its description — and the count of what was dropped. The
// count is the part that keeps this honest: a suppressed dump says how big
// it was, so nobody later reads a quiet log as a log with nothing in it.
//
// # Why the note is keyed on the lexicon URL and not on "DJ"
//
// Measured on hardware 2026-09-13: every one of seven fatal context errors in
// a 45-minute session was the same context, `context_description: "DJ"`, and
// no ordinary playlist, album or track ever failed — they played before and
// after each failure, and librespot never crashed. The dump says why:
//
//	pages: [ ContextPage { page_url: None, next_page_url: None, tracks: [] } ]
//	metadata: { "lexicon_context_url": "hm://lexicon-session-provider/..." }
//
// One page, no tracks, and no page_url to fetch any from. DJ's tracks are
// served by the lexicon session provider named in that metadata, and the
// string `lexicon` does not appear anywhere in librespot's connect module —
// so it resolves the ordinary context path, gets nothing, and gives up.
//
// That is the general condition, and it is what this keys on: a context whose
// tracks live behind a session provider librespot does not speak. Matching
// the word "DJ" instead would be matching the one instance we happened to
// measure, and `context_description` is a server-supplied display string that
// can be localised or renamed at any time.
//
// There is nothing to fix on this side. The note exists so that the next
// person reading the log is told that in one line, rather than deducing it
// from sixty.

// relayed is one line to put in the log, and who said it: librespot's own
// output is attributed to librespot, and our summary of it is not.
type relayed struct {
	tag  string
	text string
}

// logFilter collapses librespot's multi-line debug dumps and annotates the
// failure that follows one. It holds no I/O so it can be tested against
// captured output.
type logFilter struct {
	inDump  bool
	dropped int
	uri     string
	desc    string
	lexicon bool

	// lastLexicon carries the finding past the end of the dump, because the
	// error that needs the annotation is logged AFTER the dump, not before:
	// context.rs prints the context and then returns the error, and spirc
	// logs it one line later.
	lastLexicon bool
	lastDesc    string
}

// Line reports what should be relayed for one line of librespot's stderr.
// It returns nothing while a dump is being swallowed, and more than one line
// where a summary or a note has to precede the line that triggered it.
func (f *logFilter) Line(line string) []relayed {
	if f.inDump {
		if isDumpBody(line) {
			f.harvest(line)
			f.dropped++
			return nil
		}
		return append(f.closeDump(), f.open(line)...)
	}
	return f.open(line)
}

// Flush ends a dump that the stream stopped in the middle of, so its summary
// is not lost when librespot exits mid-print.
func (f *logFilter) Flush() []relayed {
	if !f.inDump {
		return nil
	}
	return f.closeDump()
}

// open relays a line that is librespot's own, notes whether it starts a dump,
// and adds our annotation where one is warranted.
func (f *logFilter) open(line string) []relayed {
	out := []relayed{{tag: "librespot", text: line}}
	if strings.Contains(line, contextHasNoTracks) && f.lastLexicon {
		out = append(out, relayed{tag: "spotify", text: sessionProviderNote(f.lastDesc)})
		// Once said, not repeated for the same dump: the same error is logged
		// by more than one layer, and three copies of a four-line explanation
		// is the noise this file exists to remove.
		f.lastLexicon = false
	}
	if opensDump(line) {
		f.inDump, f.dropped, f.uri, f.desc, f.lexicon = true, 0, "", "", false
	}
	return out
}

// closeDump ends the swallowing and reports what was in it.
func (f *logFilter) closeDump() []relayed {
	f.inDump = false
	f.lastLexicon, f.lastDesc = f.lexicon, f.desc

	var b strings.Builder
	fmt.Fprintf(&b, "%d further lines of debug dump suppressed", f.dropped)
	if f.uri != "" {
		fmt.Fprintf(&b, "; context %s", f.uri)
	}
	if f.desc != "" {
		fmt.Fprintf(&b, " (%s)", f.desc)
	}
	if f.lexicon {
		b.WriteString("; tracks come from a session provider")
	}
	return []relayed{{tag: "spotify", text: b.String()}}
}

// harvest keeps the three fields of a dump that identify it.
func (f *logFilter) harvest(line string) {
	if f.uri == "" {
		if u := spotifyURI(line); u != "" {
			f.uri = u
		}
	}
	if f.desc == "" {
		if d := quotedValue(line, `"context_description"`); d != "" {
			f.desc = d
		}
	}
	if strings.Contains(line, lexiconKey) {
		f.lexicon = true
	}
}

// contextHasNoTracks is StateError::ContextHasNoTracks as librespot renders
// it. Matched as a substring because it arrives wrapped differently by each
// layer that reports it (`Invalid state { ... }`, `setup of state failed:
// ...`), and the wrapper is not the part that identifies it.
const contextHasNoTracks = "the provided context has no tracks"

// lexiconKey is the metadata field naming the provider that actually holds
// the tracks. Its PRESENCE is the finding; the URL itself is not used.
const lexiconKey = "lexicon_context_url"

// isDumpBody reports whether a line is a continuation of a Rust `{:#?}`
// print rather than a log line of its own.
//
// Every line env_logger writes begins with `[`, and every line of a pretty
// debug print is indented — including its closing brace, which the logger
// indents with the rest. A panic message is not indented, so a crash and its
// unindented first line still come through; an indented backtrace frame would
// be counted rather than printed, which is why the count is on the summary.
func isDumpBody(line string) bool {
	if line == "" {
		return true
	}
	switch line[0] {
	case ' ', '\t':
		return true
	}
	return line == "}" || line == "]"
}

// opensDump reports whether a log line ends by opening a structure that the
// following lines will fill in.
func opensDump(line string) bool {
	if !strings.HasPrefix(line, "[") {
		return false
	}
	t := strings.TrimRight(line, " \t")
	return strings.HasSuffix(t, "{") || strings.HasSuffix(t, "[")
}

// spotifyURI returns the first Spotify URI on the line, or "".
func spotifyURI(line string) string {
	i := strings.Index(line, "spotify:")
	if i < 0 {
		return ""
	}
	u := line[i:]
	if j := strings.IndexAny(u, `"',) `); j >= 0 {
		u = u[:j]
	}
	// `spotify:` alone, or `context://spotify:...` truncated to nothing
	// useful, is not worth reporting as an identity.
	if strings.Count(u, ":") < 2 {
		return ""
	}
	return u
}

// quotedValue returns the double-quoted value following key on the line.
func quotedValue(line, key string) string {
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	rest = rest[j+1:]
	k := strings.Index(rest, `"`)
	if k < 0 {
		return ""
	}
	return rest[:k]
}

// sessionProviderNote is the line that replaces the deduction.
//
// It says what cannot work and what still does, because the second half is
// the part that stops somebody restarting a healthy endpoint: the process
// does not crash and ordinary playback is untouched.
func sessionProviderNote(desc string) string {
	what := "this context"
	if desc != "" {
		what = fmt.Sprintf("%q", desc)
	}
	return fmt.Sprintf("%s cannot play through Connect: its tracks are served by "+
		"a session provider (%s) that librespot does not implement, so the "+
		"context resolves with no tracks and no page to fetch any from. The "+
		"device drops out of Connect until something else is selected. "+
		"Ordinary playlists, albums and tracks are unaffected and librespot "+
		"is not at fault — nothing here restarts it.", what, lexiconKey)
}
