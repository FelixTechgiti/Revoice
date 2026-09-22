package endpoint

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wilbowes/EchoMuse/internal/platform"
)

// ShimPath is where the controller installs gaishim.so, the getaddrinfo
// replacement the endpoints are loaded with on emOS. Beside the programs it
// serves, and it must match `em_endpoint_bins.KINDS["gaishim"].dest`. There is
// a test on the controller side. A disagreement installs a perfectly good
// library somewhere nothing looks, and reports success for it.
const ShimPath = "/data/local/bin/gaishim.so"

// PreloadVar is the linker's, not ours. Android 5.1's linker splits it on
// spaces and colons alike.
const PreloadVar = "LD_PRELOAD"

// Seams, so both platforms can be driven from a host that is neither.
var (
	resolverBase = platform.Base
	resolverStat = os.Stat
)

// Resolver says whether the endpoints on THIS device need the getaddrinfo
// shim, and whether it is there.
//
// # Why the endpoints need one at all
//
// On emOS, bionic's `getaddrinfo` resolves nothing and its `gethostbyname`
// resolves everything (#263). Both go to emOS's DNS proxy, the proxy answers
// getaddrinfo with netd's serialisation byte for byte, and Amazon's bionic
// rejects it — along with every other shape netd could have sent. librespot
// and shairport-sync both die at startup on a name lookup, which is the whole
// of why Spotify Connect and AirPlay are dead on an emOS device.
//
// `gaishim.so` answers getaddrinfo itself, over gethostbyname. It is preloaded
// into the endpoint processes rather than fixed in emOS because emOS ships
// only inside a boot image the user assembles and flashes, and this has to
// reach a device that is already running.
//
// # Why it is never preloaded under FireOS
//
// There, netd answers and getaddrinfo works. Preloading the shim would
// replace a full resolver with a deliberately small one — no AAAA, no search
// domains, no /etc/hosts — to fix a fault that platform does not have. So the
// gate is `base_os`, and a device whose base is UNKNOWN is treated as FireOS,
// for the reason the whole `base_os` gate uses that default: absence must not
// change the behaviour of the existing fleet.
type Resolver struct {
	// Needed says this device's libc cannot resolve a name without help.
	Needed bool
	// Present says the library is installed and readable.
	Present bool
	Size    int64
	Path    string
	// Reason is empty when Present, and otherwise one of the vocabulary
	// em_endpoint_bins.STATUS_REASONS carries.
	Reason string
}

// ResolverStatus stats the shim and answers for this device. Cheap enough to
// call per endpoint session, which is what makes an install take effect on the
// next restart rather than on the next reboot.
func ResolverStatus() Resolver {
	r := Resolver{Needed: resolverBase() == platform.EmOS, Path: ShimPath}

	info, err := resolverStat(ShimPath)
	switch {
	case err != nil:
		r.Reason = "not_installed"
	case info.IsDir():
		r.Reason = "not_a_file"
	case info.Mode()&0o444 == 0:
		// A shared object is mapped, not run, so the executable bit is
		// not the thing to check — readable is. Checking for +x instead
		// would refuse a perfectly good library that arrived with 0644,
		// which is what a plain file copy produces.
		r.Reason = "not_readable"
	default:
		r.Present = true
		r.Size = info.Size()
	}
	return r
}

// Env returns the environment an endpoint subprocess should run with: the one
// given, plus LD_PRELOAD when this device both needs the shim and has it.
//
// An existing LD_PRELOAD is extended rather than replaced. Nothing in this
// firmware sets one today, but a preload that silently dropped somebody else's
// is the kind of thing that is only ever discovered from the far side.
func (r Resolver) Env(env []string) []string {
	if !r.Needed || !r.Present {
		return env
	}
	out := make([]string, 0, len(env)+1)
	set := false
	for _, kv := range env {
		if name, val, ok := strings.Cut(kv, "="); ok && name == PreloadVar {
			if val == "" {
				kv = PreloadVar + "=" + r.Path
			} else if !hasPath(val, r.Path) {
				kv = PreloadVar + "=" + r.Path + ":" + val
			}
			set = true
		}
		out = append(out, kv)
	}
	if !set {
		out = append(out, PreloadVar+"="+r.Path)
	}
	return out
}

func hasPath(list, want string) bool {
	for _, f := range strings.FieldsFunc(list, func(c rune) bool {
		return c == ':' || c == ' '
	}) {
		if f == want {
			return true
		}
	}
	return false
}

// Report rides the register message as resolver_status, beside spotify_status
// and airplay_status and for the same reason: the capability says this
// firmware knows how to use a shim, and only a stat says whether the file is
// there. `needed` is carried too, because the controller must not offer to
// install it on a device that does not want it — and must not read its absence
// there as a fault.
func (r Resolver) Report() map[string]any {
	rep := map[string]any{
		"binary": r.Path,
		"ok":     r.Present,
		"needed": r.Needed,
	}
	if r.Present {
		rep["size"] = r.Size
	} else {
		rep["reason"] = r.Reason
	}
	// Present AND refused is a fourth state the two booleans cannot express,
	// and it is the one that looks healthy from every side.
	if refusal := PreloadRefusal(); refusal != "" {
		rep["preload_error"] = refusal
	}
	if st := SelfTest(); st != "" {
		rep["selftest"] = st
	}
	return rep
}

// ── whether the linker will actually take it ────────────────────────────────

// PreloadProbe asks Android's dynamic linker to load the shim INTO THE
// PROGRAM THAT WILL ACTUALLY RUN IT, and returns its complaint — empty when
// the library loaded cleanly.
//
// # Why this exists
//
// A REFUSED PRELOAD IS SILENT. The linker writes a warning, drops the
// library and runs the program anyway, so an endpoint that cannot load the
// shim behaves in every observable way like one that never had it: it starts,
// it fails to resolve, it exits, it retries. Measured on 2026-09-21 — the
// shim was installed, md5-verified, and librespot went on failing with a
// byte-identical error for four restarts, and nothing anywhere could say
// which of three things was happening.
//
// # Why the endpoint's own binary, and nothing else
//
// The first version of this probe ran `/system/bin/sh -c :`, on the reasoning
// that it is dynamically linked where busybox is static. That was right about
// busybox and wrong about the shell, and the device said so in one line:
//
//	CANNOT LINK EXECUTABLE DEPENDENCIES: "/data/local/bin/gaishim.so"
//	is 32-bit instead of 64-bit
//
// `/system/bin/sh` on biscuit is aarch64, exactly as `/system/bin/ping` is —
// the trap #263 had already written down and this walked into one level up.
// A 64-bit probe cannot answer for a 32-bit endpoint in either direction: it
// rejects a correct library, and it would accept one built for the wrong ABI.
//
// So the only instrument that answers the question is the binary whose
// `exec` the answer is about. It is passed in rather than chosen here,
// because the caller is the one that knows which file it is about to run —
// a device may have the classic receiver or the AirPlay 2 one, at different
// paths.
func PreloadProbe(shim, binary string) (linkerErr, selfTest string) {
	if binary == "" {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--version")
	// SELFTEST makes the shim resolve a name through its own getaddrinfo at
	// load time and write the answer to stderr — from inside the endpoint's
	// own process, under the real bionic. Unset everywhere else, so the
	// library is inert in normal operation.
	cmd.Env = append(os.Environ(),
		PreloadVar+"="+shim,
		"GAISHIM_SELFTEST="+SelfTestHost)
	var errbuf bytes.Buffer
	cmd.Stderr = &errbuf
	// stdout is discarded on purpose: `--version` prints there and says
	// nothing about linking.
	err := cmd.Run()

	linkerErr = linkerFailure(errbuf.String())
	selfTest = selfTestLine(errbuf.String())
	if linkerErr == "" && selfTest == "" && err != nil && ctx.Err() != nil {
		linkerErr = "probe timed out running " + binary
	}
	return linkerErr, selfTest
}

// SelfTestHost is deliberately the name librespot dies on. A device that
// resolves it through the shim and still cannot reach Spotify has separated
// the resolver from everything downstream of it, which no other single
// measurement does.
const SelfTestHost = "clienttoken.spotify.com"

var probeTimeout = 10 * time.Second

// A linker FAILURE, not merely a linker line — and the difference cost a
// wrong verdict on 2026-09-21.
//
// Android 5.1 prints `WARNING: linker: <file>: unused DT entry: type 0x...`
// for almost everything it loads, including librespot itself and every
// library beside it. Treating any line containing "linker:" as a refusal
// reported a library that had loaded perfectly well as REFUSED, which is
// worse than saying nothing: it sent the search away from the real fault.
//
// So this matches what the linker says when it actually gives up. The
// alternative — listing the benign messages — is a list that grows with
// somebody else's code.
func linkerFailure(stderr string) string {
	var keep []string
	for _, line := range strings.Split(stderr, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		switch {
		case strings.Contains(l, "CANNOT LINK"),
			strings.Contains(l, "could not load library"),
			strings.Contains(l, "cannot locate symbol"),
			strings.Contains(l, "cannot find "),
			strings.Contains(l, "is 32-bit instead of 64-bit"),
			strings.Contains(l, "is 64-bit instead of 32-bit"):
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "; ")
}

// The shim's own answer, if it ran. The marker is the shim's and must not
// drift from SELFTEST_MARK in gaishim.c.
func selfTestLine(stderr string) string {
	const mark = "gaishim-selftest: "
	for _, line := range strings.Split(stderr, "\n") {
		if i := strings.Index(line, mark); i >= 0 {
			return strings.TrimSpace(line[i+len(mark):])
		}
	}
	return ""
}

// ── saying so, once ─────────────────────────────────────────────────────────

var (
	logMu        sync.Mutex
	logLast      string
	logAny       bool
	lastRefusal  string
	lastSelfTest string
)

// Swapped by tests; the real linker on a device.
var resolverProbe = PreloadProbe

// PreloadRefusal is the linker's last complaint about the shim, or empty.
// Read by Report so the controller sees it without a shell session.
func PreloadRefusal() string {
	logMu.Lock()
	defer logMu.Unlock()
	return lastRefusal
}

// SelfTest is what the shim answered when it last resolved a name from inside
// an endpoint's process, or empty when it has not been asked.
func SelfTest() string {
	logMu.Lock()
	defer logMu.Unlock()
	return lastSelfTest
}

// LogResolver writes one line when the shim's state CHANGES, and nothing on
// the restarts in between.
//
// librespot restarts every 60 seconds while it cannot resolve, so a line per
// session would bury the log in the one condition where somebody is reading
// it. A line per change still reports the install the moment it lands, which
// is the event somebody is waiting for.
func LogResolver(r Resolver, binary string) {
	state := "off"
	msg := ""
	switch {
	case !r.Needed:
		state = "not_needed"
		msg = "resolver: FireOS libc resolves names itself; gaishim not used"
	case r.Present:
		// The SIZE is part of the state, and that is the point rather than
		// decoration. A replaced shim is still "installed", so keying on
		// presence alone means the file can change under a running firmware
		// and never be probed again — which is exactly what happened on
		// 2026-09-22: a new library was installed at 08:35:08 and the last
		// verdict on record was from 08:34:41, about the previous one.
		//
		// Including the size makes "a different file is there now" a state
		// change, which is precisely when the question is worth asking
		// again.
		state = "active:" + strconv.FormatInt(r.Size, 10)
	default:
		state = "missing:" + r.Reason
		// The actionable case, and the one that is otherwise invisible:
		// on emOS without this file the endpoints start, retry for ever
		// and never resolve a name.
		msg = "resolver: emOS needs gaishim.so and it is " + r.Reason +
			" at " + r.Path + " — Spotify and AirPlay cannot resolve " +
			"any name until it is installed (see #263)"
	}

	logMu.Lock()
	if logAny && logLast == state {
		logMu.Unlock()
		return
	}
	logAny, logLast = true, state
	logMu.Unlock()

	if strings.HasPrefix(state, "not_needed") {
		return // true of most of the fleet; saying it once is noise
	}

	// The file being present is not the same as the linker accepting it, and
	// the difference is invisible at run time — so it is asked HERE, on the
	// transition, rather than left to be inferred from an endpoint that keeps
	// failing. One process spawn per state change, not per restart.
	if strings.HasPrefix(state, "active:") {
		refusal, selftest := resolverProbe(r.Path, binary)
		logMu.Lock()
		lastRefusal, lastSelfTest = refusal, selftest
		logMu.Unlock()
		switch {
		case refusal != "":
			msg = "resolver: the linker REFUSED " + r.Path +
				" — the endpoints are running without it and cannot " +
				"resolve any name: " + refusal
		case selftest != "":
			// The whole answer in one line: the library loaded, and this
			// is what it returned for a real name inside the endpoint's
			// own process.
			msg = "resolver: gaishim.so loaded into " + binary +
				" — self-test " + selftest
		default:
			// Loaded, and it did not answer. Said plainly rather than
			// reported as success: not looking is not evidence.
			msg = "resolver: gaishim.so loaded into " + binary +
				" but the self-test produced no line — the library may not " +
				"be the one being interposed"
		}
	}
	log.Print(msg)
}
