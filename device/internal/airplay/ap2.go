package airplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wilbowes/EchoMuse/internal/endpoint"
	"github.com/wilbowes/EchoMuse/internal/orphan"
)

// NqptpPath is the PTP clock daemon AirPlay 2 times against. A SECOND process
// on the device, beside shairport-sync.
//
// It is a separate program rather than a thread because that is what upstream
// ships: it needs UDP 319 and 320 to ITSELF — shairport-sync cannot hold them
// and neither can anything else — and it publishes the clock through a shared
// memory object that shairport reads. On bionic that object is a file under a
// tmpfs; see device/shairport/compat/android_shm.c.
const NqptpPath = "/data/local/bin/nqptp"

// ShmDirEnv and its default have to agree between the two processes, or each
// maps a different file and shairport reads a record that never changes —
// which presents as AirPlay 2 audio that will not synchronise rather than as
// anything to do with memory.
//
// So the firmware resolves it ONCE, in ShmDir below, and passes the answer
// explicitly to both children rather than letting each fall back to its own
// compiled-in default. Two defaults that happen to match today is not the same
// as one value: the shim's lives in C and the firmware's in Go, and nothing
// would report the day they stopped agreeing.
const (
	ShmDirEnv     = "REVOICE_SHM_DIR"
	DefaultShmDir = "/dev/revoice-shm"
)

// ShmDir is where the PTP clock record lives, for whichever child is being
// started. One resolver, so the receiver and the daemon cannot be told
// different things.
func ShmDir() string {
	if v := os.Getenv(ShmDirEnv); v != "" {
		return v
	}
	return DefaultShmDir
}

// Flavour is what an installed shairport-sync binary actually IS.
//
// There is no config key for "enable AirPlay 2" and there should not be: the
// device has one binary at BinaryPath, and whether it speaks AirPlay 2 is a
// property of how that binary was built. A toggle would be a second opinion
// about a question the file already answers — and the two could disagree,
// which is the shape of failure this project keeps naming.
type Flavour struct {
	// Version is the whole string the binary reported, kept for the log and
	// for airplay_status.
	Version string
	// AirPlay2 is true when the build carries AirPlay 2 support.
	AirPlay2 bool
	// ShmVersion is nqptp's shared-memory STRUCTURE version, which the binary
	// states as `-smi<N>`. It is an ABI number, not a release number: a
	// shairport built against one and an nqptp publishing another do not
	// interoperate, and neither says so in a way a user would see.
	ShmVersion int
}

// The version string is assembled by get_version_string() in shairport-sync's
// common.c, which appends one hyphenated token per compiled-in feature —
// "4.3.7-AirPlay2-smi10-alac-stdout-metadata-...". Matching on the tokens
// rather than parsing the whole thing means a build that gains a feature does
// not stop being recognised.
var (
	reAirPlay2 = regexp.MustCompile(`(^|-)AirPlay2(-|$)`)
	reShmVer   = regexp.MustCompile(`(^|-)smi([0-9]+)(-|$)`)
)

// ParseVersion reads `shairport-sync -V` output.
//
// Tolerant of surrounding whitespace and of extra lines, because -V has
// printed more than one line in some builds and a diagnostic that breaks on a
// newline is worse than no diagnostic.
func ParseVersion(out string) Flavour {
	f := Flavour{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if f.Version == "" {
			f.Version = line
		}
		if reAirPlay2.MatchString(line) {
			f.AirPlay2 = true
			f.Version = line
		}
		if m := reShmVer.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[2]); err == nil {
				f.ShmVersion = n
			}
		}
	}
	return f
}

// DetectFlavour asks the installed binary what it is.
//
// `run` is injected so this is testable without a binary; production passes
// nil and gets exec. A binary that cannot be run at all is not an error the
// caller should act on beyond reporting it — the receiver may still start, and
// deciding otherwise from a failed `-V` would turn a diagnostic into an
// outage.
func DetectFlavour(binary string, run func(name string, args ...string) ([]byte, error)) (Flavour, error) {
	if run == nil {
		run = func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
	out, err := run(binary, "-V")
	if err != nil && len(out) == 0 {
		return Flavour{}, fmt.Errorf("airplay: cannot run %s -V: %w", binary, err)
	}
	// Output is used even when the process exited non-zero: some builds print
	// the version and then complain about something unrelated.
	return ParseVersion(string(out)), nil
}

// NqptpPlan says what to do about the clock daemon, and why.
//
// Split out from the doing so the decision can be read and tested. The reason
// string is not decoration: every branch here is a state somebody will have to
// diagnose from a dashboard, and "AirPlay 2 is installed but nqptp is not" is
// otherwise indistinguishable from "AirPlay is broken".
type NqptpPlan struct {
	Run    bool
	Reason string
}

// PlanNqptp decides whether to run the clock daemon.
//
// The rule that matters is the last one: an AirPlay 2 binary with no nqptp
// still serves CLASSIC AirPlay perfectly, so a missing clock daemon must not
// stop the receiver. Refusing to start would trade a degraded feature for no
// feature, which is the opposite of what this project's compatibility rule
// asks for.
func PlanNqptp(f Flavour, nqptpInstalled bool) NqptpPlan {
	switch {
	case !f.AirPlay2:
		return NqptpPlan{Run: false, Reason: "classic build — no clock daemon needed"}
	case !nqptpInstalled:
		return NqptpPlan{Run: false, Reason: "airplay2_no_nqptp"}
	default:
		return NqptpPlan{Run: true, Reason: "airplay2"}
	}
}

// Nqptp supervises the clock daemon.
//
// Deliberately its own small supervisor rather than a second Client: it has no
// audio, no music plane, no config file and no metadata. What it shares with
// the receiver is the restart discipline, and that is copied rather than
// abstracted — two supervisors in one package that read the same way are
// cheaper than one that has to be parameterised for both.
type Nqptp struct {
	Path   string
	ShmDir string

	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	restarts  int
	lastExit  string
	proc      *os.Process
	startedAt time.Time
}

// ErrNoNqptp means the clock daemon is not installed.
var ErrNoNqptp = errors.New("airplay: nqptp is not installed on this device")

// FileRunnable reports whether a path is a file this device could exec, with
// a three-way answer rather than a bool: a missing file and a broken one want
// different things said, and the whole point of the endpoint status reporting
// is that "not installed" and "installed and unusable" must not look alike.
func FileRunnable(path string) (bool, string) {
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return false, "not_installed"
	case info.IsDir():
		return false, "not_a_file"
	case info.Mode()&0o111 == 0:
		return false, "not_executable"
	default:
		return true, "ok"
	}
}

// NqptpAvailable reports whether the daemon is present and executable, with
// the same three-way answer Report() gives for shairport-sync: a missing file
// and a broken feature must not look alike.
func NqptpAvailable(path string) (bool, string) { return FileRunnable(path) }

// AP2BinaryPath is where the controller installs the AirPlay 2 receiver.
//
// **A SECOND path, not a replacement for the first, and that is the whole
// design.** One file per protocol means the device can be switched between
// them by a setting rather than by re-pushing 1.5MB over a link measured at
// 5-7% packet loss — and switched BACK the same way, which for an
// implementation nobody has ever run on this hardware is the property that
// matters most. It also keeps the store's rule intact: one kind per
// destination, so nothing has to guess which of two files at one path is the
// one somebody meant.
//
// The cost is ~1.5MB of /data holding a receiver that is not running. Against
// a rollback that needs no transfer at all, on a device whose only management
// path is the network, that is not a close call.
const AP2BinaryPath = "/data/local/bin/shairport-sync-ap2"

// SelectedName says which receiver a setting asks for, for a log line. It
// describes the REQUEST, not what is installed — ResolveBinary answers that,
// and the two differ exactly while an install is outstanding.
func SelectedName(preferAP2 bool) string {
	if preferAP2 {
		return "AirPlay 2 (" + AP2BinaryPath + ")"
	}
	return "classic (" + BinaryPath + ")"
}

// BinaryChoice is which receiver this device will run, and why.
//
// The reason travels because every branch here is a state somebody has to
// diagnose from a dashboard: "asked for AirPlay 2 and the file is not there"
// and "did not ask for AirPlay 2" are the same running binary and completely
// different situations, and only one of them is somebody waiting for an
// install that never happened.
type BinaryChoice struct {
	Path     string
	AirPlay2 bool
	Reason   string
}

// ResolveBinary picks the receiver to run.
//
// **The setting selects WHICH FILE; the file still answers WHAT IT IS.** That
// division is what keeps this from being the toggle this package has always
// refused: nothing here claims a binary speaks AirPlay 2 — `DetectFlavour`
// asks the binary that, on whichever path this returns, and the clock daemon
// is started off that answer. So the two cannot disagree. A setting that said
// "this device speaks AirPlay 2" could.
//
// Falling back to classic when the AirPlay 2 file is absent is the
// compatibility rule rather than politeness: the alternative is a device that
// asked for AirPlay 2, has not been given it yet, and serves no AirPlay at
// all in the meantime — degrading to no behaviour instead of to the old one.
func ResolveBinary(classic, ap2 string, preferAP2 bool) BinaryChoice {
	if !preferAP2 {
		return BinaryChoice{Path: classic, Reason: "classic (AirPlay 2 not enabled)"}
	}
	ok, why := FileRunnable(ap2)
	if ok {
		return BinaryChoice{Path: ap2, AirPlay2: true, Reason: "airplay2"}
	}
	return BinaryChoice{Path: classic,
		Reason: "classic — AirPlay 2 is enabled but " + ap2 + " is " + why}
}

// Start brings the daemon up. Idempotent.
func (n *Nqptp) Start() error {
	if n.Path == "" {
		n.Path = NqptpPath
	}
	if ok, why := NqptpAvailable(n.Path); !ok {
		return fmt.Errorf("%w (%s: %s)", ErrNoNqptp, n.Path, why)
	}

	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.running = true
	n.cancel = cancel
	n.restarts = 0
	n.mu.Unlock()

	// An orphan holds 319 and 320, and nothing else on the device can take
	// them back. Exactly the failure internal/orphan was written for — an
	// orphaned shairport-sync held TCP 5000 for two hours while every panel
	// reported the endpoint healthy — and worse here, because nqptp exits
	// immediately when it cannot bind, so the supervisor below would simply
	// loop for ever against a process it cannot see.
	//
	// At START rather than only at shutdown, for that package's reason: a
	// cleanup on the way out cannot run after kill -9, after a panic, or on
	// the supervisor's own restart path.
	if k := orphan.Takeover(n.Path); k > 0 {
		log.Printf("[airplay] stopped %d orphaned nqptp instance(s) left by a previous run", k)
	}

	log.Printf("[airplay] nqptp enabled (shm %s)", n.shmDir())
	go n.supervise(ctx)
	return nil
}

// Stop takes it down. Idempotent.
func (n *Nqptp) Stop() {
	n.mu.Lock()
	cancel := n.cancel
	n.running = false
	n.cancel = nil
	n.mu.Unlock()
	if cancel != nil {
		cancel()
		log.Printf("[airplay] nqptp disabled")
	}
}

// shmDir is the resolver plus a test seam. Production leaves ShmDir empty and
// gets the same answer the receiver is given; a test sets it to a temp dir.
func (n *Nqptp) shmDir() string {
	if n.ShmDir != "" {
		return n.ShmDir
	}
	return ShmDir()
}

// Running says the supervisor is up — not that a process exists right now.
// The distinction is endpoint.Health's, and it is the one that mattered when
// an orphaned shairport-sync held port 5000 for two hours while every panel
// reported the endpoint fine.
func (n *Nqptp) Running() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running
}

// Restarts counts exits since Start, so a climbing number separates "briefly
// between sessions" from "failing every few seconds". LastExit carries the
// reason, because `exit status 1` — a port it cannot bind — against
// `signal: killed` is the whole difference.
func (n *Nqptp) Restarts() (int, string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.restarts, n.lastExit
}

// Health is what the clock daemon says about itself, in the same three fields
// the streaming endpoints use — and it is here for the reason the endpoint
// package was written at all: "installed" is not "working", and nqptp's one
// likely failure is invisible from every other panel.
//
// It cannot bind UDP 319 and 320 if anything else holds them, and it exits
// immediately when it cannot. From the outside that is a device with an
// AirPlay 2 binary, an nqptp file of the right size, and audio that will not
// synchronise — with the restart counter climbing once a second as the only
// thing anywhere that says so.
func (n *Nqptp) Health() endpoint.Health {
	n.mu.Lock()
	defer n.mu.Unlock()
	h := endpoint.Health{
		Enabled:  n.running,
		Alive:    n.proc != nil,
		Restarts: n.restarts,
		LastExit: n.lastExit,
	}
	if n.proc != nil && !n.startedAt.IsZero() {
		h.UptimeS = int(time.Since(n.startedAt).Seconds())
	}
	return h
}

// Restart re-executes the daemon so a REPLACED BINARY takes effect, and
// reports whether there was anything to restart.
//
// Same reasoning as the receiver's Restart, and the same failure behind it: a
// rename replaces a directory entry, not the inode a process is executing, so
// installing a new nqptp over a running one succeeds, matches its md5, and
// leaves the old code running indefinitely. nqptp needs this more than the
// receiver does, because nothing a user can see changes when it happens —
// AirPlay 2 goes on playing out of sync.
func (n *Nqptp) Restart() bool {
	n.mu.Lock()
	proc, running := n.proc, n.running
	n.mu.Unlock()
	if !running || proc == nil {
		// Not enabled, or between attempts: the next exec opens the new
		// inode by itself, and claiming a restart that did not happen is
		// what this whole path exists to stop.
		return false
	}
	log.Printf("[airplay] restarting nqptp to pick up a replaced binary")
	n.mu.Lock()
	n.proc = nil
	n.mu.Unlock()
	_ = proc.Kill()
	return true
}

func (n *Nqptp) supervise(ctx context.Context) {
	backoff := restartMin
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		err := n.session(ctx)
		if ctx.Err() != nil {
			return
		}

		n.mu.Lock()
		n.restarts++
		if err != nil {
			n.lastExit = err.Error()
		} else {
			n.lastExit = "exited cleanly"
		}
		n.mu.Unlock()

		if err != nil {
			// Worth a line every time rather than once: the failure this is
			// most likely to hit is UDP 319/320 already held, which is
			// permanent until something else lets go, and a single line at
			// the top of an uptime is a line nobody finds.
			log.Printf("[airplay] nqptp exited: %v", err)
		}
		if time.Since(start) > 30*time.Second {
			backoff = restartMin
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > restartMax {
			backoff = restartMax
		}
	}
}

func (n *Nqptp) session(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, n.Path)
	// The shm directory is passed EXPLICITLY rather than inherited, so the
	// value this process decided on is the value the daemon uses. Both halves
	// of the clock interface must agree on the path; inheriting leaves that to
	// whatever the supervisor's environment happened to hold.
	// nqptp resolves `localhost` for its own control port, which on emOS goes
	// to the router and comes back NXDOMAIN (#219) — and reaches bionic's
	// getaddrinfo, which answers nothing there either (#263). The shim covers
	// both: loopback is answered inside it, before any lookup.
	res := endpoint.ResolverStatus()
	endpoint.LogResolver(res)
	cmd.Env = res.Env(append(os.Environ(), ShmDirEnv+"="+n.shmDir()))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	n.mu.Lock()
	n.proc = cmd.Process
	n.startedAt = time.Now()
	n.mu.Unlock()
	go relayLog(stdout)
	go relayLog(stderr)
	err = cmd.Wait()
	// Cleared here rather than by the supervisor, so Alive is false for the
	// whole of the backoff rather than only until the next loop iteration.
	// The distinction is the one Health exists for: a daemon failing every
	// second is Enabled and not Alive, and a sample that caught it mid-loop
	// would say it was fine.
	n.mu.Lock()
	n.proc = nil
	n.startedAt = time.Time{}
	n.mu.Unlock()
	return err
}
