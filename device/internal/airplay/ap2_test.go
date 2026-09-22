package airplay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wilbowes/EchoMuse/internal/netfilter"
)

// The strings are real shapes from shairport-sync's get_version_string()
// (common.c), which appends one hyphenated token per compiled-in feature. The
// point of testing against them rather than against a parser of our own design
// is that a build gaining a feature must not stop being recognised.
func TestParseVersionReadsTheFeatureTokens(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		ap2    bool
		shm    int
		verSub string
	}{
		{
			name:   "the classic build we ship today",
			out:    "4.3.7-mbedTLS-stdout-metadata-sysconfdir:/etc\n",
			ap2:    false,
			shm:    0,
			verSub: "4.3.7",
		},
		{
			name:   "an AirPlay 2 build",
			out:    "4.3.7-AirPlay2-smi10-mbedTLS-stdout-metadata-sysconfdir:/etc\n",
			ap2:    true,
			shm:    10,
			verSub: "AirPlay2",
		},
		{
			name: "a git-described AirPlay 2 build",
			out:  "4.3.7-27-gabcdef0-AirPlay2-smi11-alac-stdout-metadata\n",
			ap2:  true,
			shm:  11,
		},
		{
			// -V has printed more than one line in some builds, and a
			// diagnostic that breaks on a newline is worse than none.
			name: "extra lines around it",
			out:  "\n  4.3.7-AirPlay2-smi10-stdout  \nsome other note\n",
			ap2:  true,
			shm:  10,
		},
		{
			// The token has to be bounded by hyphens or ends. A build whose
			// name merely CONTAINED the word must not read as AirPlay 2 —
			// that would start a clock daemon for a binary that cannot use it.
			name: "a lookalike token is not a match",
			out:  "4.3.7-NotAirPlay2ish-stdout\n",
			ap2:  false,
			shm:  0,
		},
		{
			name: "empty output",
			out:  "",
			ap2:  false,
			shm:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ParseVersion(tc.out)
			if f.AirPlay2 != tc.ap2 {
				t.Errorf("AirPlay2 = %v, want %v (from %q)", f.AirPlay2, tc.ap2, tc.out)
			}
			if f.ShmVersion != tc.shm {
				t.Errorf("ShmVersion = %d, want %d", f.ShmVersion, tc.shm)
			}
			if tc.verSub != "" && !strings.Contains(f.Version, tc.verSub) {
				t.Errorf("Version = %q, want it to contain %q", f.Version, tc.verSub)
			}
		})
	}
}

// The binary is the authority on what it is, so a failure to ask must not be
// read as an answer. Detect returns an error rather than a zero Flavour that
// says "classic".
func TestDetectFlavourUsesOutputEvenOnANonZeroExit(t *testing.T) {
	f, err := DetectFlavour("/nonexistent", func(string, ...string) ([]byte, error) {
		return []byte("4.3.7-AirPlay2-smi10-stdout\n"), errors.New("exit status 1")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.AirPlay2 || f.ShmVersion != 10 {
		t.Errorf("got %+v, want AirPlay2 with shm 10", f)
	}
}

func TestDetectFlavourFailsWhenItCannotRunAtAll(t *testing.T) {
	_, err := DetectFlavour("/nonexistent", func(string, ...string) ([]byte, error) {
		return nil, errors.New("no such file")
	})
	if err == nil {
		t.Fatal("want an error when the binary cannot be run and said nothing")
	}
}

// The rule this pins is the one that would otherwise be got wrong under
// pressure: an AirPlay 2 binary with no clock daemon still serves CLASSIC
// AirPlay, so a missing nqptp must not stop the receiver.
func TestPlanNqptp(t *testing.T) {
	classic := Flavour{Version: "4.3.7-stdout"}
	ap2 := Flavour{Version: "4.3.7-AirPlay2-smi10", AirPlay2: true, ShmVersion: 10}

	if p := PlanNqptp(classic, false); p.Run {
		t.Error("a classic build must not start a clock daemon")
	}
	if p := PlanNqptp(classic, true); p.Run {
		t.Error("a classic build must not start a clock daemon even if one is installed")
	}

	p := PlanNqptp(ap2, false)
	if p.Run {
		t.Error("nqptp cannot be started when it is not installed")
	}
	if p.Reason != "airplay2_no_nqptp" {
		t.Errorf("reason = %q, want a reason a dashboard can show", p.Reason)
	}

	if p := PlanNqptp(ap2, true); !p.Run || p.Reason != "airplay2" {
		t.Errorf("got %+v, want Run with reason airplay2", p)
	}
}

func TestNqptpAvailableSeparatesMissingFromBroken(t *testing.T) {
	dir := t.TempDir()

	if ok, why := NqptpAvailable(filepath.Join(dir, "nope")); ok || why != "not_installed" {
		t.Errorf("missing: ok=%v why=%q", ok, why)
	}

	sub := filepath.Join(dir, "adir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, why := NqptpAvailable(sub); ok || why != "not_a_file" {
		t.Errorf("directory: ok=%v why=%q", ok, why)
	}

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, why := NqptpAvailable(plain); ok || why != "not_executable" {
		t.Errorf("non-executable: ok=%v why=%q", ok, why)
	}

	if err := os.Chmod(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, why := NqptpAvailable(plain); !ok || why != "ok" {
		t.Errorf("executable: ok=%v why=%q", ok, why)
	}
}

func TestNqptpStartRefusesWhenNotInstalled(t *testing.T) {
	n := &Nqptp{Path: filepath.Join(t.TempDir(), "nqptp")}
	err := n.Start()
	if err == nil {
		t.Fatal("want an error")
	}
	if !errors.Is(err, ErrNoNqptp) {
		t.Errorf("err = %v, want it to wrap ErrNoNqptp", err)
	}
	if n.Running() {
		t.Error("a refused start must not leave the supervisor marked running")
	}
}

// Both processes must map the same object or shairport reads a record that
// never changes — which looks like a clock that will not sync, not like a
// path problem. The value is resolved in one place for that reason.
func TestNqptpShmDirPrefersTheExplicitValue(t *testing.T) {
	t.Setenv(ShmDirEnv, "/from/env")

	if got := (&Nqptp{ShmDir: "/explicit"}).shmDir(); got != "/explicit" {
		t.Errorf("shmDir = %q, want the explicit value", got)
	}
	if got := (&Nqptp{}).shmDir(); got != "/from/env" {
		t.Errorf("shmDir = %q, want the environment value", got)
	}

	t.Setenv(ShmDirEnv, "")
	if got := (&Nqptp{}).shmDir(); got != DefaultShmDir {
		t.Errorf("shmDir = %q, want %q", got, DefaultShmDir)
	}
}

func TestNqptpStopIsIdempotent(t *testing.T) {
	n := &Nqptp{Path: filepath.Join(t.TempDir(), "nqptp")}
	n.Stop()
	n.Stop()
	if n.Running() {
		t.Error("Running after Stop")
	}
}

// The state this exists to make visible: enabled, not alive, restarts
// climbing. On the receiver that was an orphan holding TCP 5000 for two hours
// while every panel read healthy; here it is nqptp failing to bind UDP 319,
// which is silent in a different way — classic AirPlay goes on working, so
// nothing a user can hear says anything is wrong.
func TestNqptpHealthSeparatesEnabledFromAlive(t *testing.T) {
	n := &Nqptp{Path: filepath.Join(t.TempDir(), "nqptp")}

	if h := n.Health(); h.Enabled || h.Alive {
		t.Errorf("a daemon that was never started reads %+v", h)
	}

	n.mu.Lock()
	n.running = true
	n.restarts = 42
	n.lastExit = "exit status 1"
	n.mu.Unlock()

	h := n.Health()
	if !h.Enabled {
		t.Error("Enabled must follow the supervisor, not the process")
	}
	if h.Alive {
		t.Error("Alive must be false while no process exists")
	}
	if h.Restarts != 42 || h.LastExit != "exit status 1" {
		t.Errorf("Health = %+v, want the restart count and the reason carried", h)
	}
	if h.UptimeS != 0 {
		t.Errorf("UptimeS = %d, want none for a process that is not running", h.UptimeS)
	}
}

// Nothing to restart must answer false rather than claiming an action, for
// the reason the controller's own note depends on it: "restarted, the new
// binary is live" about a process still executing the old inode is the exact
// failure the message exists to end, with a reassuring sentence on top.
func TestNqptpRestartClaimsNothingItDidNotDo(t *testing.T) {
	n := &Nqptp{Path: filepath.Join(t.TempDir(), "nqptp")}
	if n.Restart() {
		t.Error("a daemon that is not enabled has nothing to restart")
	}

	n.mu.Lock()
	n.running = true
	n.mu.Unlock()
	if n.Restart() {
		t.Error("enabled but between attempts: the next exec opens the new inode by itself")
	}
}

func TestPortEnvNamesMatchTheShim(t *testing.T) {
	// The names exist twice — once here and once in the C the binary is built
	// from — because one side is Go and the other is a container build. A
	// disagreement is silent in the worst way: shairport-sync would go back to
	// kernel-chosen ports and the firewall would name an empty range, which is
	// the failure this whole mechanism removes, restored by a typo.
	src, err := os.ReadFile(filepath.Join("..", "..", "shairport", "compat", "ap2_ports.h"))
	if err != nil {
		t.Fatalf("cannot read the shim's header, so the names cannot be compared: %v", err)
	}
	h := string(src)
	for _, want := range []struct{ macro, value string }{
		{"EM_AP2_PORT_BASE_ENV", AP2PortBaseEnv},
		{"EM_AP2_PORT_COUNT_ENV", AP2PortCountEnv},
	} {
		if !strings.Contains(h, `#define `+want.macro+` "`+want.value+`"`) {
			t.Fatalf("%s is %q here, and ap2_ports.h does not define %s as that — "+
				"the binary would read a variable the firmware never sets",
				want.macro, want.value, want.macro)
		}
	}
}

func TestSessionPortsAreNotTheClassicRTPRange(t *testing.T) {
	// Two ranges, both handed to the same process, and they must not overlap:
	// the classic RTP ports are bound by name at startup, so a session socket
	// placed on one of them would fail to bind for the life of the receiver
	// and the walk would quietly move on — a range that is one port smaller
	// than the rule says, every time.
	loA, hiA := netfilter.AirPlayUDPBase, netfilter.AirPlayUDPBase+netfilter.AirPlayUDPRange-1
	loB, hiB := netfilter.AirPlay2SessionBase, netfilter.AirPlay2SessionBase+netfilter.AirPlay2SessionCount-1
	if loA <= hiB && loB <= hiA {
		t.Fatalf("the classic RTP range %d:%d and the AirPlay 2 session range %d:%d overlap",
			loA, hiA, loB, hiB)
	}
	if netfilter.AirPlay2SessionCount < 4 {
		t.Fatalf("a session binds four sockets and the range holds %d",
			netfilter.AirPlay2SessionCount)
	}
}
