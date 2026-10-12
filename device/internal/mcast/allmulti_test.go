package mcast

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestTheFlagsWordIsReadTheWayTheKernelWritesIt(t *testing.T) {
	for _, tc := range []struct {
		flags string
		want  bool
	}{
		// What /sys/class/net/wlan0/flags actually holds, with the newline.
		{"0x1003\n", false}, // UP|BROADCAST|MULTICAST, no ALLMULTI
		{"0x1203\n", true},  // the same with IFF_ALLMULTI
		{"0x1203", true},    // no newline
		{"1203\n", true},    // no 0x, since nothing promises the prefix
		{"0X1203\n", true},  // upper case
		{"0x200\n", true},   // the bit alone
		{"0x1fff\n", true},  // every flag set
		{"0x0\n", false},    // a down interface
	} {
		got, err := HasAllMulti(tc.flags)
		if err != nil {
			t.Errorf("HasAllMulti(%q) errored: %v", tc.flags, err)
			continue
		}
		if got != tc.want {
			t.Errorf("HasAllMulti(%q) = %v, want %v", tc.flags, got, tc.want)
		}
	}
}

// An unreadable value must NOT answer false. False means "set it", and this
// writes the whole flags word of the interface the device is managed over.
func TestAnUnparseableFlagsWordIsAnErrorAndNotFalse(t *testing.T) {
	for _, bad := range []string{"", "   ", "\n", "nonsense", "0x", "0xzz", "-1"} {
		if _, err := HasAllMulti(bad); err == nil {
			t.Errorf("HasAllMulti(%q) returned no error; a value nobody could "+
				"read must not be answered with 'so write to the interface'", bad)
		}
	}
}

func TestTheKeeperSetsTheFlagOnlyWhenItIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  string
		active bool
		want   bool
	}{
		{"missing and something is running", "0x1003\n", true, true},
		{"already set", "0x1203\n", true, false},
		{"missing but nothing is running", "0x1003\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sets := 0
			k := &MultiKeeper{
				Read:   func() (string, error) { return tc.flags, nil },
				Set:    func() error { sets++; return nil },
				Active: func() bool { return tc.active },
			}
			if got := k.Tick(); got != tc.want {
				t.Errorf("Tick = %v, want %v", got, tc.want)
			}
			if (sets > 0) != tc.want {
				t.Errorf("Set called %d times, want any = %v", sets, tc.want)
			}
		})
	}
}

func TestAFailedReadWritesNothing(t *testing.T) {
	sets := 0
	k := &MultiKeeper{
		Read:   func() (string, error) { return "", errors.New("no such file") },
		Set:    func() error { sets++; return nil },
		Active: func() bool { return true },
	}
	if k.Tick() || sets != 0 {
		t.Fatalf("a failed read led to a write; SIOCSIFFLAGS takes the whole "+
			"flags word, so writing one nobody read can clear IFF_UP (sets=%d)", sets)
	}
}

func TestAFailingSetIsCountedAndSaidOnce(t *testing.T) {
	k := &MultiKeeper{
		Read:   func() (string, error) { return "0x1003\n", nil },
		Set:    func() error { return errors.New("operation not permitted") },
		Active: func() bool { return true },
	}
	for i := 0; i < 4; i++ {
		if k.Tick() {
			t.Fatalf("tick %d reported success on a failing setter", i)
		}
	}
	if k.failed != 4 {
		t.Errorf("failed = %d, want 4", k.failed)
	}
	if k.Sets() != 0 {
		t.Errorf("Sets = %d, want 0", k.Sets())
	}
}

// The flag is host-side state and a re-association can drop it, so the keeper
// has to notice the flag going away again rather than setting it once.
func TestItSetsTheFlagAgainAfterSomethingClearsIt(t *testing.T) {
	flags := "0x1003\n"
	k := &MultiKeeper{
		Read:   func() (string, error) { return flags, nil },
		Set:    func() error { flags = "0x1203\n"; return nil },
		Active: func() bool { return true },
	}
	if !k.Tick() {
		t.Fatal("the first tick did not set the flag")
	}
	if k.Tick() {
		t.Fatal("set the flag again while it was already on")
	}
	flags = "0x1003\n" // a re-association drops it
	if !k.Tick() {
		t.Fatal("did not set the flag again after it was cleared")
	}
	if k.Sets() != 2 {
		t.Errorf("Sets = %d, want 2", k.Sets())
	}
}

func TestTheFlagsPathIsTheOneTheKernelPublishes(t *testing.T) {
	if got := FlagsPath("wlan0"); got != "/sys/class/net/wlan0/flags" {
		t.Errorf("FlagsPath = %q", got)
	}
}

// The firmware has to actually run it. cmd/server.go is read as text because
// `cmd` is behind the `server` build tag, so a host `go test ./...` compiles
// none of it — the same reason netfilter checks its own wiring this way.
func TestTheFirmwareKeepsTheInterfaceInAllMulti(t *testing.T) {
	b, err := os.ReadFile("../../cmd/server.go")
	if err != nil {
		t.Fatalf("cannot read cmd/server.go, so the wiring cannot be checked: %v", err)
	}
	src := string(b)
	for _, want := range []string{"mcast.MultiKeeper{", "mcast.SetAllMulti", "mcast.FlagsPath"} {
		if !strings.Contains(src, want) {
			t.Errorf("cmd/server.go does not mention %q: the keeper is built "+
				"and never runs, which is the fault it was written for", want)
		}
	}
}
