package spotify

import (
	"strings"
	"testing"
	"time"

	"github.com/wilbowes/EchoMuse/internal/musicplane"
)

// The order below is verbatim from the device, 2026-09-13 13:11:57 — and the
// order is the whole point. librespot announces the fallback track BEFORE it
// discovers the context is empty, so a flag cleared by `Loading` and set by
// the error ends up SET for the claim that follows one second later. A test
// that fed these two lines the other way round would pass while the fix did
// nothing.
const (
	loadingLine  = `[2026-09-13T13:11:57Z INFO  librespot_playback::player] Loading <COMEBACCC> with Spotify URI <spotify:track:1n7Z2V4TgFMmNLejaDj9rm>`
	noTracksLine = `[2026-09-13T13:11:57Z ERROR librespot_connect::spirc] Invalid state { the provided context has no tracks }`
)

func relay(c *Client, lines ...string) {
	c.relayLog(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

func TestAFailedContextIsRememberedEvenThoughItsTrackWasAnnouncedFirst(t *testing.T) {
	c := New(Options{}, nil, &fakePlane{})
	relay(c, loadingLine, noTracksLine)
	if !c.contextFailed() {
		t.Fatal("the error arrives after the Loading, so it must win")
	}
}

func TestATrackThatActuallyLoadsClearsIt(t *testing.T) {
	c := New(Options{}, nil, &fakePlane{})
	relay(c, loadingLine, noTracksLine)
	relay(c, `[2026-09-13T13:12:14Z INFO  librespot_playback::player] Loading <Come Get Her> with Spotify URI <spotify:track:1Ser4X0TKttOvo8bgdytTP>`)
	if c.contextFailed() {
		t.Fatal("a track that loaded must clear the flag, or Spotify could never preempt again")
	}
}

// The fix itself: the same audio, against a plane somebody else is using.
func TestAudioFromAFailedContextDoesNotEvictALiveSource(t *testing.T) {
	p := &fakePlane{busy: true}
	c := New(Options{}, nil, p)
	relay(c, loadingLine, noTracksLine)

	claim := musicplane.NewIdleClaim(p, time.Hour)
	if c.feed(claim) {
		t.Fatal("wrote over a source that was playing")
	}
	if p.claims != 0 {
		t.Errorf("used the evicting claim %d times", p.claims)
	}
	if p.ifFree == 0 {
		t.Error("did not even ask for a free plane")
	}
}

func TestAudioFromAFailedContextStillTakesAnIdlePlane(t *testing.T) {
	// Dropping it would mean a user who picks DJ with nothing else playing
	// gets silence where they used to get the fallback track. The rule
	// withholds an eviction, not playback.
	p := &fakePlane{}
	c := New(Options{}, nil, p)
	relay(c, loadingLine, noTracksLine)

	if !c.feed(musicplane.NewIdleClaim(p, time.Hour)) {
		t.Fatal("refused a plane nobody was using")
	}
}

func TestOrdinaryAudioStillPreempts(t *testing.T) {
	// The regression that would matter most: starting Spotify while AirPlay
	// plays must still hand the speaker to Spotify.
	p := &fakePlane{busy: true}
	c := New(Options{}, nil, p)
	relay(c, loadingLine)

	if !c.feed(musicplane.NewIdleClaim(p, time.Hour)) {
		t.Fatal("a healthy Spotify session must still take the plane")
	}
	if p.claims == 0 {
		t.Error("took it without the evicting claim")
	}
}
