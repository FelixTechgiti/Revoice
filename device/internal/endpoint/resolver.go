package endpoint

import (
	"log"
	"os"
	"strings"
	"sync"

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
	return rep
}

// ── saying so, once ─────────────────────────────────────────────────────────

var (
	logMu   sync.Mutex
	logLast string
	logAny  bool
)

// LogResolver writes one line when the shim's state CHANGES, and nothing on
// the restarts in between.
//
// librespot restarts every 60 seconds while it cannot resolve, so a line per
// session would bury the log in the one condition where somebody is reading
// it. A line per change still reports the install the moment it lands, which
// is the event somebody is waiting for.
func LogResolver(r Resolver) {
	state := "off"
	msg := ""
	switch {
	case !r.Needed:
		state = "not_needed"
		msg = "resolver: FireOS libc resolves names itself; gaishim not used"
	case r.Present:
		state = "active"
		msg = "resolver: gaishim.so preloaded into the endpoints (" +
			r.Path + ")"
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
	defer logMu.Unlock()
	if logAny && logLast == state {
		return
	}
	logAny, logLast = true, state
	if state == "not_needed" {
		return // true of most of the fleet; saying it once is noise
	}
	log.Print(msg)
}
