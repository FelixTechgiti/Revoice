// Package netfilter opens the device's own ports in FireOS's firewall.
//
// **FireOS ships a default-deny INPUT policy with an allowlist of Amazon's
// own ports, and ours are not on it.** Read off a live device 2026-09-12:
//
//	-P INPUT DROP
//	-A INPUT -i wlan0 -p tcp -m state --state RELATED,ESTABLISHED -j ACCEPT
//	-A INPUT -i wlan0 -p udp -m udp --dport 5353 -j ACCEPT      <- mDNS
//	-A INPUT -i wlan0 -p tcp -m tcp --dport 4070 -j ACCEPT      <- Alexa
//	-A INPUT -i wlan0 -p udp -m udp --dport 5000 -j ACCEPT      <- UDP, not TCP
//	-A INPUT -p icmp -m state --state RELATED,ESTABLISHED -j ACCEPT
//	... policy DROP 605 packets, 89226 bytes
//
// That one page explains everything #77 spent weeks on. mDNS is allowed, so
// the announcements go out and are heard and every on-device measurement
// looks perfect. ESTABLISHED is allowed, so the three control planes — which
// the DEVICE dials — work faultlessly. And a phone answering that
// advertisement is a NEW inbound connection, which is dropped: Spotify
// Connect and AirPlay both need the phone to call the speaker.
//
// Note `udp dpt:5000` in that list. Amazon opened UDP 5000 for something of
// their own; shairport-sync's RTSP is **TCP** 5000, so even the port that
// looks allowed is not the one we need. A rule read at a glance would have
// sent the next person away satisfied.
//
// # Why iptables here rather than somewhere else
//
// This is the Linux interface, not an Android one — `iptables` is the kernel's
// own, and the project's direction says to prefer exactly that. Nothing here
// is FireOS-specific except the REASON it is needed.
//
// **Under emOS the rules are still written, and the first version of this
// comment said otherwise.** emOS has no default-deny policy — that is Amazon's
// init script, which does not run — but it mounts Amazon's `/system`, so
// `/system/bin/iptables` is there and works. So `Sync` inserts four ACCEPT
// rules into a table whose policy is already ACCEPT: no-ops, costing a handful
// of execs at startup and on each config push. Left that way deliberately
// rather than gated on the base, because the question this package answers is
// "is this port reachable", which is about the TABLE and not about which
// userspace booted — and a device that one day runs a firewall under emOS then
// works without anybody remembering this file exists.
//
// `ErrUnavailable` therefore covers a base with no iptables binary at all,
// which is neither of the two we ship. It is not the emOS path.
//
// # The rules this may write, and the ones it must never
//
// A firewall on a device whose ONLY management path is the network is a
// loaded gun. Three constraints, all enforced by test:
//
//   - **Only `-I INPUT` and `-D INPUT`, with a fully specified rule.** Never
//     `-P` (a policy change can strand the device for good), never `-F`
//     (flushing takes Amazon's allowlist with it, and the control plane's
//     ESTABLISHED rule with that), never `-X`.
//   - **Every rule names an interface and a port.** A bare ACCEPT is not a
//     fix, it is the removal of a firewall.
//   - **Idempotent.** `-C` before `-I`, because this runs on every start and
//     on every config push, and a duplicate rule per reconnect is a table
//     that grows for the life of the device.
//
// # And the ports have to be PINNED, or no rule can match
//
// librespot picks a random zeroconf port per start and shairport-sync uses
// its own defaults. A firewall rule cannot be written against a number that
// changes, so both are pinned here and passed to the daemons from these same
// constants — one definition, so the rule and the listener cannot drift.
package netfilter

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// The interface the device is reachable on. Amazon's own rules are scoped to
// it; ours are too, so a rule can never widen anything on another link.
// Iface is the radio every rule here names, and the one both endpoints
// announce on. Exported because mDNS responders on this platform have to be
// TOLD which interface they are on — see IfaceIPv4 — and a second copy of the
// string in another package is one that eventually disagrees with the
// firewall.
const Iface = "wlan0"

const iface = Iface

// Ports we pin so a rule can name them. Changing one means changing the
// daemon's argument as well — they are read from here, which is the point.
const (
	// SpotifyZeroconfPort is librespot's discovery HTTP listener
	// (`--zeroconf-port`). Random by default, which is unfirewallable.
	SpotifyZeroconfPort = 36000

	// AirPlayRTSPPort is shairport-sync's control port. 5000 is the
	// conventional AirPlay port and shairport's own default; clients take
	// the real number from the SRV record, so this is a choice rather than
	// a protocol constant.
	AirPlayRTSPPort = 5000

	// AirPlayUDPBase/Range is shairport-sync's RTP range (`udp_port_base`,
	// `udp_port_range`) — audio, control and timing. The control port alone
	// gets a session started and no sound out of it.
	AirPlayUDPBase  = 6001
	AirPlayUDPRange = 10

	// NqptpPortA/B are the PTP ports the AirPlay 2 clock daemon needs
	// INBOUND. Fixed by the protocol rather than chosen: 319 is event and
	// 320 is general, and nqptp requires exclusive use of both.
	//
	// Unlike everything else here they are not our numbers to pick, so a rule
	// naming them cannot drift from a daemon argument — there is no argument.
	NqptpPortA = 319
	NqptpPortB = 320

	// MDNSPort is the mDNS port, fixed by RFC 6762 rather than chosen. Both
	// responders bind it; see MDNSRule for why a rule has to name it.
	MDNSPort = 5353

	// AirPlay2SessionBase/Count is the range an AirPlay 2 session's own
	// sockets are taken from. Chosen to sit immediately above the classic RTP
	// range so the two read as one block, and pinned here because the
	// alternative is what the long comment below used to describe: ports the
	// kernel picks, which no rule can name.
	//
	// shairport-sync is told the same numbers through the environment and
	// walks the range itself — see device/shairport/compat/ap2_ports.h. One
	// definition, so the rule and the listener cannot drift.
	//
	// Ten for a protocol that binds four sockets per session: enough for a
	// session to start while the previous one's sockets are still in
	// TIME_WAIT, which is the case a range of four would fail on and nobody
	// would connect to a range.
	AirPlay2SessionBase  = 6011
	AirPlay2SessionCount = 10
)

// Rule is one INPUT accept that belongs to us.
type Rule struct {
	Proto string // "tcp", "udp" or "icmp"
	Port  string // "5000", "6001:6010"; empty only for icmp
	Why   string // for the log line, so a reader knows whose port this is
}

// Runner executes one iptables invocation. Injected so the whole decision is
// testable without a firewall — the alternative is a package that can only be
// exercised on a rooted Echo, which is the one place a mistake here is
// expensive.
type Runner func(args ...string) error

// SpotifyRules / AirPlayRules are what each endpoint needs open.
func SpotifyRules() []Rule {
	return []Rule{{
		Proto: "tcp",
		Port:  fmt.Sprint(SpotifyZeroconfPort),
		Why:   "Spotify Connect discovery (librespot)",
	}}
}

func AirPlayRules() []Rule {
	return []Rule{
		{Proto: "tcp", Port: fmt.Sprint(AirPlayRTSPPort),
			Why: "AirPlay RTSP (shairport-sync)"},
		{Proto: "udp",
			Port: fmt.Sprintf("%d:%d", AirPlayUDPBase,
				AirPlayUDPBase+AirPlayUDPRange-1),
			Why: "AirPlay RTP audio/control/timing"},
	}
}

// NqptpRules are what the AirPlay 2 clock daemon needs open.
//
// Separate from AirPlayRules because it is a separate PROCESS with a separate
// installed-or-not answer: a device can have an AirPlay 2 shairport-sync and
// no nqptp, and opening PTP ports for a daemon that is not there is a hole
// with nothing behind it.
func NqptpRules() []Rule {
	return []Rule{
		{Proto: "udp", Port: fmt.Sprint(NqptpPortA),
			Why: "PTP event (nqptp, AirPlay 2)"},
		{Proto: "udp", Port: fmt.Sprint(NqptpPortB),
			Why: "PTP general (nqptp, AirPlay 2)"},
	}
}

// # AirPlay 2's per-session ports, and how they came to be nameable
//
// **This section described an unsolved problem until #79, and the paragraphs
// below are kept because the reasoning is what the answer was chosen from.**
// What changed is the last line of it: shairport-sync now takes those sockets
// from AirPlay2SessionBase/Count instead of from the kernel, so
// AirPlay2SessionRules can name them. The patch is one rename plus a wrapper
// in device/shairport/ap2/in-container.sh; the analysis stands as written.
//
// Read off rtsp.c on 2026-09-12, and unchanged between 4.3.7 and 5.5.1, so it
// is not something a version bump fixes:
//
//	conn->local_event_port = 0;          // any port   (rtsp.c:3098, :3172)
//	conn->local_buffered_audio_port = 0; // any port   (rtsp.c:3363)
//
// For every AirPlay 2 session shairport-sync binds TWO additional TCP sockets
// on KERNEL-CHOSEN ephemeral ports, tells the client their numbers in the
// SETUP response, and waits for the client to connect IN. There is no config
// option for them. So on a default-DROP firewall an AirPlay 2 session
// negotiates and then stalls — which presents as a speaker that appears,
// accepts a connection and plays nothing, rather than as a firewall.
//
// The choices, none of which is free, and none of which should be made by
// whoever happens to be adding a rule:
//
//   - Open the ephemeral TCP range on wlan0 (`/proc/sys/net/ipv4/
//     ip_local_port_range`, typically 32768:61000). Broad, and it is a
//     speaker on somebody's home network — that is the owner's call.
//   - Narrow ip_local_port_range system-wide and firewall the small result.
//     Possible as root; it changes every other process on the device.
//   - Patch shairport-sync to take the two ports from a configured base. The
//     narrowest answer and the only one that stays narrow, at the cost of a
//     patch this build has so far avoided entirely.
//
// The third was chosen, 2026-09-22. It is the only one that stays narrow: the
// first opens roughly 28,000 ports on somebody's home network for a music
// session, and the second changes every other process on the device to avoid
// doing so. The cost is the first real patch to shairport-sync's own source in
// this build — guarded so that it fails the build rather than silently
// stopping to apply, because a binary that looks right and stalls every
// session is exactly the failure being removed.

// AirPlay2SessionRules are the per-session sockets, which are TCP and UDP over
// the same range — event and buffered audio are SOCK_STREAM, ap2_control is
// SOCK_DGRAM, and which lands where is up to the order a session binds them
// in. So both protocols are opened over the whole range rather than guessed
// at: a rule that is right for the sockets one session happened to bind is a
// rule that is wrong for the next.
//
// Separate from AirPlayRules for NqptpRules' reason — a classic receiver binds
// none of these, and opening twenty ports for sockets that will never exist is
// a hole with nothing behind it.
func AirPlay2SessionRules() []Rule {
	r := fmt.Sprintf("%d:%d", AirPlay2SessionBase,
		AirPlay2SessionBase+AirPlay2SessionCount-1)
	return []Rule{
		{Proto: "tcp", Port: r,
			Why: "AirPlay 2 session event and buffered-audio sockets"},
		{Proto: "udp", Port: r,
			Why: "AirPlay 2 session control socket"},
	}
}

// MDNSRule opens inbound mDNS, which is what lets the device be ASKED.
//
// **Neither half of the project opened it, because each believed the other
// had** — measured on emOS 2026-09-22 (#298). FireOS's Amazon allowlist
// carries `udp dpt:5353`, which is why this package never needed the rule and
// why the header comment above lists mDNS among the things already allowed.
// emOS has a default-deny policy of its own and allows `udp SPT:5353` only —
// mDNS responses, so the device can find its controller. A query arrives on
// the destination port, so on emOS every query to both responders was dropped.
//
// The failure is silent in the worst direction: announcements are outbound and
// unaffected, so the service appears in a picker, is heard by the whole
// segment, and vanishes when the client's cache expires with no way to refresh
// it. `/proc/net/igmp` has the group and `netstat` has the socket throughout.
//
// So the rule is written here rather than inherited. It is a no-op wherever the
// port is already open, which is the cheaper half of the trade: a duplicate
// ACCEPT costs one exec at startup, and an assumption about somebody else's
// allowlist cost #77, #298 and every hour spent on either.
//
// Gated on an endpoint being enabled, like the rest — the device's own
// discovery of its controller is the client side and rides the source-port
// rule, so a device advertising nothing does not need this open.
func MDNSRule() Rule {
	return Rule{Proto: "udp", Port: fmt.Sprint(MDNSPort),
		Why: "mDNS queries, so Spotify and AirPlay can be asked as well as heard"}
}

// IGMPProto is IGMP by NUMBER, because the devices this runs on have no
// /etc/protocols and their iptables answers `unknown protocol "igmp"` — checked
// on hardware 2026-09-22.
const IGMPProto = "2"

// IGMPRule lets the device answer the router's membership queries.
//
// IGMP is how a multicast membership is KEPT. The router asks periodically who
// still wants each group; a station that does not answer is dropped from the
// switch's snooping table, and then the group stops being delivered to it —
// which is #142, a device that works for a few minutes after anything joins
// and then hears nothing.
//
// A default-deny INPUT chain drops IGMP, because it is neither TCP, UDP nor
// ICMP and nothing had ever named it. So a device that depends on multicast
// was silently unable to keep its membership.
//
// **Whether this is what CAUSES #142 is not established, and the honest
// version is worth keeping**: with this rule added by hand, exactly ONE IGMP
// packet arrived in an hour while mDNS went from 44,217 to 47,473 — so the
// queries are rare on that router, and one of them being dropped is a
// plausible cause rather than a measured one. What IS measured is the repair
// in internal/mcast, which re-joins and works. This is the cheaper half:
// answering a query keeps the membership that the re-join would otherwise have
// to restore.
//
// It names no port because IGMP HAS none — the protocol number is the whole of
// what can be narrowed, the same position ICMP is in.
func IGMPRule() Rule {
	return Rule{Proto: IGMPProto,
		Why: "IGMP membership queries, so the router keeps delivering multicast"}
}

// PingRule lets the device answer a ping.
//
// FireOS accepts only RELATED,ESTABLISHED ICMP, so an echo request — which is
// NEW — is dropped, and `icmp_echo_ignore_all` is 0, meaning the kernel would
// have answered. That distinction cost an afternoon: a device that does not
// answer ping reads as "off the network", and this one was never off it.
// Being pingable is the cheapest diagnosis anyone has.
func PingRule() Rule {
	return Rule{Proto: "icmp", Why: "answer ping, so the device can be diagnosed"}
}

// spec is the fully-specified rule, without the -I/-C/-D verb.
func (r Rule) spec() []string {
	a := []string{"INPUT", "-i", iface, "-p", r.Proto}
	switch {
	case r.Proto == "icmp":
		a = append(a, "--icmp-type", "echo-request")
	case r.Proto == IGMPProto:
		// Nothing to narrow: IGMP carries no ports, and iptables has no
		// `-m 2` match module to add. The protocol number IS the narrowing.
	case r.Port != "":
		a = append(a, "-m", r.Proto, "--dport", r.Port)
	}
	return append(a, "-j", "ACCEPT")
}

// String is what the log shows, and it is the rule itself rather than a
// summary: somebody reading a support bundle has to be able to check it.
func (r Rule) String() string {
	return strings.Join(r.spec(), " ") + "   # " + r.Why
}

// Sync makes exactly `want` present and everything else this package knows
// about absent, so turning an endpoint off closes its port.
//
// Errors are logged, never returned: this runs beside the code that starts
// the endpoints, and a firewall that could not be adjusted must not stop a
// device from booting. The endpoint then runs unreachable, which is the
// behaviour every device had before this existed.
//
// **No iptables at all is an ORDINARY answer, not a failure.** emOS has no
// such firewall, and that is the whole of what it has to do here. It is said
// once per process, at info level, because the log relay forwards lines
// matching "could not" to the controller — so a per-rule complaint on every
// config push would put four warnings into somebody's Home Assistant log
// every reconnect, about a device that has nothing wrong with it.
func Sync(run Runner, want []Rule) {
	if run == nil {
		return
	}
	wanted := map[string]bool{}
	for _, r := range want {
		wanted[r.String()] = true
		if !ensure(run, r) {
			return
		}
	}
	for _, r := range All() {
		if !wanted[r.String()] {
			remove(run, r)
		}
	}
}

// All is every rule this package may ever write — the set Sync removes from.
// Listed explicitly rather than remembered across runs: the process restarts,
// and a rule we forgot we added is a port left open for a service that is off.
func All() []Rule {
	out := append([]Rule{}, SpotifyRules()...)
	out = append(out, AirPlayRules()...)
	out = append(out, NqptpRules()...)
	out = append(out, AirPlay2SessionRules()...)
	out = append(out, MDNSRule())
	out = append(out, IGMPRule())
	return append(out, PingRule())
}

// maxDupes bounds the delete loop below. Reaching it would mean a table with
// eight copies of one rule, which is a fault of its own; stopping is better
// than looping against an iptables that answers success for ever.
const maxDupes = 8

// ensure makes the rule present EXACTLY ONCE, by deleting every copy and
// inserting one.
//
// The obvious implementation is `-C` then `-I`, and it was the first one. It
// rests on `-C` working, which is an assumption about a binary we do not ship
// and cannot test here: an iptables where `-C` is missing or answers wrongly
// turns "check, then insert" into "insert", on a path that runs at every
// start and every config push — a table that grows for the life of the
// device. Delete-then-insert needs only `-D`, which every iptables has had
// for ever, and it REPAIRS duplicates rather than merely not creating them.
//
// Reports false if the firewall is not there at all, so the caller stops
// rather than saying the same thing about every remaining rule.
func ensure(run Runner, r Rule) bool {
	had := drop(run, r)
	if err := run(append([]string{"-I"}, r.spec()...)...); err != nil {
		if errors.Is(err, ErrUnavailable) {
			noFirewallOnce.Do(func() {
				log.Printf("[netfilter] no iptables on this device — " +
					"nothing to open, which is normal under emOS")
			})
			return false
		}
		log.Printf("[netfilter] could not open %s/%s (%s): %v — the endpoint "+
			"will run but nothing on the network can reach it",
			r.Proto, r.Port, r.Why, err)
		return true
	}
	if had == 0 {
		log.Printf("[netfilter] opened %s", r)
	}
	return true
}

func remove(run Runner, r Rule) {
	if drop(run, r) > 0 {
		log.Printf("[netfilter] closed %s/%s (%s)", r.Proto, r.Port, r.Why)
	}
}

// drop deletes every copy of the rule and reports how many there were. A
// failing `-D` is the ordinary "it was not there" answer and is silent —
// every call site reaches this with the rule quite possibly absent.
func drop(run Runner, r Rule) int {
	n := 0
	for n < maxDupes {
		if err := run(append([]string{"-D"}, r.spec()...)...); err != nil {
			break
		}
		n++
	}
	return n
}

// ErrUnavailable says there is no iptables to talk to. Distinguished from a
// rule that would not apply, because the two want opposite responses: one is
// a device with no firewall and nothing to do, the other is a firewall that
// refused us and left a port shut.
var ErrUnavailable = errors.New("no iptables on this device")

var noFirewallOnce sync.Once

// binary resolves iptables ONCE, by absolute path first.
//
// Bare-name exec works for `tinymix` and `getprop` here, so /system/bin is on
// PATH — but this runs on a device where being wrong means a port stays shut
// with nothing said about it, and the absolute path removes the question.
// LookPath stays as the fallback so a platform that puts it elsewhere still
// works.
var binary = func() string {
	for _, p := range []string{
		"/system/bin/iptables",
		"/sbin/iptables",
		"/system/xbin/iptables",
		"/usr/sbin/iptables",
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("iptables"); err == nil {
		return p
	}
	return ""
}()

// Exec is the real runner.
func Exec(args ...string) error {
	if binary == "" {
		return ErrUnavailable
	}
	out, err := exec.Command(binary, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %v: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IfaceIPv4 returns Iface's IPv4 address, or "" if it has none yet.
//
// It exists because librespot's --zeroconf-interface takes an ADDRESS where
// shairport-sync's `interface` takes a name, and because neither responder can
// find the interface by itself on this platform: both reach getifaddrs through
// device/shairport/compat/android_ifaddrs.c, bionic declaring the real one
// __INTRODUCED_IN(24), and where that list comes back unusable they bind 5353,
// join 224.0.0.251 and announce NOTHING — healthy-looking sockets, invisible
// speaker (#298).
//
// net.InterfaceByName is deliberately the source: it is pure Go over netlink
// and does not go through the shim, so this answer is independent of the bug
// it works around.
func IfaceIPv4() string {
	ifi, err := net.InterfaceByName(Iface)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if v4 := ipn.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}
