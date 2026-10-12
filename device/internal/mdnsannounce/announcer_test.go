package mdnsannounce

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func filled(t *testing.T) *Cache {
	t.Helper()
	c := NewCache(0)
	if !c.Observe(announcement(120, "192.168.178.42")) {
		t.Fatal("the test cache did not fill")
	}
	return c
}

func TestATickSendsEverythingHeld(t *testing.T) {
	c := NewCache(0)
	c.Observe(announcement(120, "192.168.178.42"))
	c.Observe(records(3, ".other.local."))
	var sent int
	a := &Announcer{Cache: c, Send: func([]byte) error { sent++; return nil }}
	if n := a.Tick(); n != 2 {
		t.Errorf("Tick = %d, want 2", n)
	}
	if sent != 2 || a.Sent() != 2 || a.Rounds() != 1 {
		t.Errorf("sent=%d Sent=%d Rounds=%d", sent, a.Sent(), a.Rounds())
	}
}

// With both endpoints off there is nothing published, so announcing would
// advertise services that are not running — the picker entry would appear and
// then refuse the connection.
func TestNothingIsAnnouncedWhileNothingIsPublished(t *testing.T) {
	sent := 0
	a := &Announcer{
		Cache:  filled(t),
		Send:   func([]byte) error { sent++; return nil },
		Active: func() bool { return false },
	}
	if a.Tick() != 0 || sent != 0 {
		t.Errorf("announced while nothing was running (sent=%d)", sent)
	}
}

// Every held announcement carries an A record. Repeating one after the device
// has taken a different address points every browser on the LAN at whoever
// holds the old one now.
func TestAChangedAddressDropsWhatIsHeld(t *testing.T) {
	c := filled(t)
	addr := "192.168.178.42"
	a := &Announcer{
		Cache:    c,
		Send:     func([]byte) error { return nil },
		Identity: func() (string, error) { return addr, nil },
	}
	if a.Tick() != 1 {
		t.Fatal("the first tick did not announce")
	}
	addr = "192.168.178.77"
	if n := a.Tick(); n != 0 {
		t.Errorf("Tick = %d after the address changed, want 0", n)
	}
	if c.Len() != 0 {
		t.Errorf("Len = %d after the address changed, want 0", c.Len())
	}
}

// Failure to look is not evidence. An interface being reconfigured reads as
// an error, and dropping the cache on it would silence the device for the
// rest of the outage.
func TestAnUnreadableAddressIsNotAChangedOne(t *testing.T) {
	c := filled(t)
	fail := false
	a := &Announcer{
		Cache: c,
		Send:  func([]byte) error { return nil },
		Identity: func() (string, error) {
			if fail {
				return "", errors.New("no such device")
			}
			return "192.168.178.42", nil
		},
	}
	a.Tick()
	fail = true
	if a.Tick() != 0 {
		t.Error("announced on an address that could not be read")
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d: an unreadable address emptied the cache", c.Len())
	}
	fail = false
	if a.Tick() != 1 {
		t.Error("did not resume once the address could be read again")
	}
}

// A rename restarts the responder under a new name, and every held packet
// still carries the old one. Announcing those leaves a ghost in every picker
// on the LAN that connects to nothing, for as long as the firmware runs.
func TestARenameDropsWhatIsHeld(t *testing.T) {
	c := filled(t)
	name := "192.168.178.42\x00Studio"
	a := &Announcer{
		Cache:    c,
		Send:     func([]byte) error { return nil },
		Identity: func() (string, error) { return name, nil },
	}
	if a.Tick() != 1 {
		t.Fatal("the first tick did not announce")
	}
	name = "192.168.178.42\x00Wohnzimmer"
	if n := a.Tick(); n != 0 {
		t.Errorf("Tick = %d after the rename, want 0", n)
	}
	if c.Len() != 0 {
		t.Errorf("Len = %d after the rename, want 0", c.Len())
	}
}

// The FIRST identity read is not a change. Dropping the cache on it would
// discard the startup announcements, which are the only ones either responder
// sends unprompted and therefore the only ones there are.
func TestTheFirstIdentityReadIsNotAChange(t *testing.T) {
	c := filled(t)
	a := &Announcer{
		Cache:    c,
		Send:     func([]byte) error { return nil },
		Identity: func() (string, error) { return "192.168.178.42\x00Studio", nil },
	}
	if a.Tick() != 1 || c.Len() != 1 {
		t.Error("the first round dropped what it was given")
	}
}

func TestAFailingSendIsCountedAndTheNextRoundStillRuns(t *testing.T) {
	a := &Announcer{
		Cache: filled(t),
		Send:  func([]byte) error { return errors.New("network is unreachable") },
	}
	for i := 0; i < 3; i++ {
		if a.Tick() != 0 {
			t.Fatalf("round %d reported a send that failed", i)
		}
	}
	if a.Failures() != 3 {
		t.Errorf("Failures = %d, want 3", a.Failures())
	}
	if a.Rounds() != 0 {
		t.Errorf("Rounds = %d, want 0", a.Rounds())
	}
}

func TestAnEmptyCacheSendsNothing(t *testing.T) {
	sent := 0
	a := &Announcer{
		Cache: NewCache(0),
		Send:  func([]byte) error { sent++; return nil },
	}
	for i := 0; i < 10; i++ {
		a.Tick()
	}
	if sent != 0 {
		t.Errorf("sent %d packets from an empty cache", sent)
	}
}

// The firmware has to actually run it. cmd/server.go is read as text because
// `cmd` is behind the `server` build tag, so a host `go test ./...` compiles
// none of it — the same reason the netfilter and mcast packages check their
// own wiring this way.
func TestTheFirmwareStartsTheAnnouncer(t *testing.T) {
	b, err := os.ReadFile("../../cmd/server.go")
	if err != nil {
		t.Fatalf("cannot read cmd/server.go, so the wiring cannot be checked: %v", err)
	}
	src := string(b)
	for want, why := range map[string]string{
		"mdnsannounce.NewCache":                   "the cache is never built",
		"mdnsannounce.Listen":                     "the socket is never opened, so nothing is ever heard",
		".Announcer(":                             "nothing is built to repeat what was heard",
		".Run(nil, mdnsannounce.DefaultInterval)": "the ticker never runs, so nothing is ever re-sent",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("cmd/server.go does not mention %q: %s", want, why)
		}
	}
}
