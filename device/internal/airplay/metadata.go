package airplay

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strconv"
	"strings"
)

// shairport-sync's metadata stream, and the one item in it we want.
//
// # The format
//
// With `--with-metadata` shairport-sync writes a stream of XML-ish items to a
// pipe. Each is:
//
//	<item><type>73736e63</type><code>70766f6c</code><length>25</length>
//	<data encoding="base64">LTE1LjAwMDAwMCwtMzAuMDAwMDAwLC0zMC4wMDAwMDAsMC4wMDAwMDA=</data></item>
//
// `type` and `code` are four ASCII bytes each, written as hex — `73736e63` is
// "ssnc" (shairport's own namespace) and `70766f6c` is "pvol". The payload is
// base64 and, for pvol, is four comma-separated floats:
//
//	airplay_volume,volume,lowest_volume,highest_volume
//
// The FIRST is the one that matters: AirPlay's own volume in dB, spanning
// -30..0 with -144 for mute. The rest are shairport's derived values and say
// nothing we cannot work out ourselves.
//
// # Parsed by hand rather than with encoding/xml
//
// The stream is not a document: it has no root element and never ends, so a
// decoder that wants one would block for ever waiting for a close tag that is
// not coming. Scanning for `</item>` is what the format actually is.
//
// # Everything unrecognised is skipped in silence
//
// shairport emits dozens of item types — track titles, cover art, session
// begin and end — and this reads one. An unknown item is not an error and
// must not be logged as one: cover art alone would fill the log with a line
// per track change.

const (
	metaTypeSSNC = "ssnc"
	metaCodePVOL = "pvol"

	// The transport codes, read off shairport-sync 4.3.7's own
	// `metadata_hub.c` at the pin rather than from memory. `pres` and `pbeg`
	// both mean playing there; `paus` and `pend` are the two that matter here.
	metaCodePBEG = "pbeg"
	metaCodePRES = "pres"
	metaCodePAUS = "paus"
	metaCodePEND = "pend"
)

// PlayState is what the sender last did with its transport.
//
// Three values rather than two, because a PAUSE and a STOP want the same
// thing from the music plane and say different things about what comes next:
// a stop ends the session, a pause is somebody who intends to carry on.
// Nothing here acts on that difference yet, and collapsing them now would
// throw away the only chance to.
type PlayState int

const (
	// PlayStatePlaying — audio is coming, or already is.
	PlayStatePlaying PlayState = iota
	// PlayStatePaused — the sender pressed pause.
	PlayStatePaused
	// PlayStateStopped — the session ended.
	PlayStateStopped
)

func (p PlayState) String() string {
	switch p {
	case PlayStatePlaying:
		return "playing"
	case PlayStatePaused:
		return "paused"
	case PlayStateStopped:
		return "stopped"
	}
	return "unknown"
}

// item is one parsed metadata item, with type and code decoded from hex.
type item struct {
	Type string
	Code string
	Data []byte
}

// scanItems reads the metadata stream and calls fn for every complete item.
// It returns when the reader ends.
//
// Bounded by bufio's own buffer rather than reading an item into memory
// blindly: cover art arrives on this stream and can be megabytes, and this
// runs on a device with 512MB shared with Android. An item too large for the
// buffer is skipped rather than accumulated, which is right — the only item
// we read is under a hundred bytes.
func scanItems(r io.Reader, fn func(item)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 8*1024), 64*1024)
	sc.Split(splitItems)
	for sc.Scan() {
		if it, ok := parseItem(sc.Text()); ok {
			fn(it)
		}
	}
}

// splitItems is a bufio.SplitFunc yielding one `<item>…</item>` per token.
func splitItems(data []byte, atEOF bool) (advance int, token []byte, err error) {
	const closing = "</item>"
	if i := strings.Index(string(data), closing); i >= 0 {
		end := i + len(closing)
		return end, data[:end], nil
	}
	if atEOF {
		return len(data), nil, nil
	}
	return 0, nil, nil
}

// parseItem pulls the three fields out of one item. ok=false for anything
// malformed or not carrying data — an ordinary condition, not an error.
func parseItem(s string) (item, bool) {
	t, ok := hexField(s, "type")
	if !ok {
		return item{}, false
	}
	c, ok := hexField(s, "code")
	if !ok {
		return item{}, false
	}
	raw, ok := between(s, `<data encoding="base64">`, "</data>")
	if !ok {
		// **An item with no payload is legitimate and is KEPT, with nil
		// Data.** It used to be discarded here, with the comment "session
		// begin/end carry nothing but their code — and nothing here wants
		// one" — true of the only consumer there was, and load-bearing for
		// the next one: `paus`, `pend`, `pbeg` and `pres` are precisely the
		// payload-less items, so the transport state was thrown away one
		// layer below the code meant to read it (#387).
		//
		// Deciding what a caller wants is the caller's; this answers whether
		// the item parsed. `parseVolumeDB` refuses nil anyway, so the volume
		// half is untouched.
		return item{Type: t, Code: c}, true
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return item{}, false
	}
	return item{Type: t, Code: c, Data: data}, true
}

// hexField reads <name>hex</name> and decodes it to its four ASCII bytes.
func hexField(s, name string) (string, bool) {
	raw, ok := between(s, "<"+name+">", "</"+name+">")
	if !ok {
		return "", false
	}
	b, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func between(s, open, close string) (string, bool) {
	i := strings.Index(s, open)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// parseVolumeDB reads the AirPlay volume out of a pvol payload.
//
// Only the first field. The other three are shairport's own derived numbers,
// and reading them would be taking its opinion of our hardware's range —
// which it has none of, since the stdout backend exposes no volume control.
func parseVolumeDB(data []byte) (float64, bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(string(data)), ",")
	if first == "" {
		return 0, false
	}
	db, err := strconv.ParseFloat(first, 64)
	if err != nil {
		return 0, false
	}
	return db, true
}

// WatchMetadata reads the stream and reports what this device acts on: every
// AirPlay volume change in decibels, and every transport change. Blocks until
// r ends. Either callback may be nil.
//
// **Both ride ONE stream and therefore one reader.** They were two features
// arriving months apart, which is exactly how a second pipe, a second FIFO
// and a second goroutine get built for a file that is already open — and a
// FIFO with two readers delivers each byte to one of them, so the second
// reader would not be a duplicate, it would be a thief.
func WatchMetadata(r io.Reader, onVolume func(db float64), onPlay func(PlayState)) {
	scanItems(r, func(it item) {
		if it.Type != metaTypeSSNC {
			return
		}
		switch it.Code {
		case metaCodePVOL:
			if onVolume == nil {
				return
			}
			if db, ok := parseVolumeDB(it.Data); ok {
				onVolume(db)
			}
		case metaCodePBEG, metaCodePRES:
			if onPlay != nil {
				onPlay(PlayStatePlaying)
			}
		case metaCodePAUS:
			if onPlay != nil {
				onPlay(PlayStatePaused)
			}
		case metaCodePEND:
			if onPlay != nil {
				onPlay(PlayStateStopped)
			}
		}
	})
}

// VolumeFromMetadata is WatchMetadata with no transport handler, kept because
// the volume is the half with hardware verification behind it.
func VolumeFromMetadata(r io.Reader, onVolume func(db float64)) {
	WatchMetadata(r, onVolume, nil)
}
