package mcast

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// This file keeps the interface in ALLMULTI, which is what actually makes
// inbound multicast arrive on this hardware.
//
// # The fault, measured on Studio 2026-10-12
//
// Inbound multicast stops for good while every local indicator says it should
// be arriving: `/proc/net/igmp` holds 224.0.0.251 with two sockets,
// `/proc/net/dev_mcast` carries `01005e0000fb`, `ip maddr` agrees, outbound
// multicast reaches four other hosts, and the controller link is clean at
// 1.4ms. `Udp: InDatagrams` from /proc/net/snmp stays frozen across a browse
// six other Spotify hosts answer.
//
// It is the Echo's own address filter, not the access point:
//
//	857856..857859  frozen at 1449228, firewall rule for 224.0.0.251 at 1 pkt
//	                ip link set wlan0 allmulticast on
//	857862          1449231, rule 2
//	857864          1449234, rule 3
//	857868          1449237, rule 4
//	857873          1449240, rule 5
//
// Traffic within two seconds, nothing else touched. The AP had been
// forwarding the group all along and the interface was discarding it — the
// MediaTek driver's offloaded filter goes stale while the host-side list it
// was built from does not, so nothing on the device reports the difference.
//
// # What this replaces, and what was wrong with it
//
// An earlier version of this package sent unsolicited IGMP membership reports
// on a cadence, on the theory that the AP had aged its snooping entry out. It
// shipped in v2.63.1-fx.1 and **does not fix this**: measured the same
// session, a report pair sent by hand moved InDatagrams by zero over sixteen
// seconds. The theory came from a single earlier observation that did not
// replicate — traffic resumed nine seconds after a report, and re-running it
// showed that was coincidence. The reports are gone; `device/tools/igmpprobe`
// stays, because disproving that theory is what it is for.
//
// Restarting both endpoints does repair it, and now it is clear why: the
// membership refcount reaching 0 and then 1 makes the kernel call the
// driver's `set_rx_mode`, which re-programs the chip. That is a side effect of
// the restart rather than its purpose, and ALLMULTI reaches it without
// stopping anybody's music.
//
// # The cost, stated rather than glossed
//
// ALLMULTI means the host sees every multicast frame on the segment instead
// of the handful the filter would have passed. On a home LAN that is single
// figures per second and the CPU cost is not measurable beside the mic
// pipeline — but it is not nothing, and on a busy segment it would be a real
// cost on a board sharing 512MB with Android. It is accepted because the
// alternative measured on this device is Spotify Connect and AirPlay not
// working at all.

// IffAllMulti is IFF_ALLMULTI from <linux/if.h>: receive all multicast.
const IffAllMulti = 0x200

const (
	siocGIFFLAGS = 0x8913
	siocSIFFLAGS = 0x8914
)

// FlagsPath is where the kernel publishes an interface's flags as hex. It is
// read rather than ioctl'd for the CHECK, so the common case costs one small
// file read and the parsing is testable on a host with no interface at all.
func FlagsPath(iface string) string {
	return "/sys/class/net/" + iface + "/flags"
}

// HasAllMulti reports whether the flags word read from FlagsPath has
// IFF_ALLMULTI set. The file holds `0x1003` and a newline.
//
// An unparseable value is an ERROR rather than false: this decides whether to
// write to an interface, and "could not read" must not be answered with "so
// set it again", which would write on every tick for ever.
func HasAllMulti(flags string) (bool, error) {
	s := strings.TrimSpace(flags)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if s == "" {
		return false, fmt.Errorf("empty flags")
	}
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return false, fmt.Errorf("flags %q: %w", flags, err)
	}
	return v&IffAllMulti != 0, nil
}

// SetAllMulti turns IFF_ALLMULTI on for iface.
//
// SIOCSIFFLAGS writes the WHOLE flags word, so the current one is read first
// and the bit is OR'd in. Writing a composed value instead would clear
// IFF_UP on an interface whose only management path is that interface.
func SetAllMulti(iface string) error {
	if len(iface) >= 16 {
		return fmt.Errorf("interface name %q is too long", iface)
	}
	fd, err := syscall.Socket(syscall.AF_INET,
		syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("socket for SIOCSIFFLAGS: %w", err)
	}
	defer syscall.Close(fd)

	var ifr struct {
		name  [16]byte
		flags uint16
		_     [22]byte
	}
	copy(ifr.name[:], iface)

	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		siocGIFFLAGS, uintptr(unsafe.Pointer(&ifr))); e != 0 {
		return fmt.Errorf("SIOCGIFFLAGS %s: %w", iface, e)
	}
	if ifr.flags&IffAllMulti != 0 {
		return nil
	}
	ifr.flags |= IffAllMulti
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		siocSIFFLAGS, uintptr(unsafe.Pointer(&ifr))); e != 0 {
		return fmt.Errorf("SIOCSIFFLAGS %s: %w", iface, e)
	}
	return nil
}

// MultiKeeper holds the interface in ALLMULTI.
//
// It re-checks rather than setting once, because the flag is host-side state
// that a re-association, a supplicant reconfigure or anything else that takes
// the interface down and up can drop — the same reason the membership Watcher
// beside it re-reads instead of trusting a join.
type MultiKeeper struct {
	// Read returns the contents of FlagsPath(Iface).
	Read func() (string, error)
	// Set turns the flag on.
	Set func() error
	// Active reports whether anything needs multicast. The same gate the
	// Watcher and the Prober use: a device with both endpoints off has
	// nothing to receive, and passing every multicast frame to the host for
	// a feature nobody switched on is a cost with no benefit at all.
	Active func() bool

	Iface string

	sets   int
	failed int
	warned bool
}

// Sets is how many times the flag has been turned on — once per boot on a
// healthy device, and climbing on one where something keeps clearing it,
// which is the measurement that would name the trigger.
func (k *MultiKeeper) Sets() int { return k.sets }

// Tick performs one check and reports whether it set the flag.
func (k *MultiKeeper) Tick() bool {
	if k.Read == nil || k.Set == nil {
		return false
	}
	if k.Iface == "" {
		k.Iface = Iface
	}
	if k.Active != nil && !k.Active() {
		return false
	}
	flags, err := k.Read()
	if err != nil {
		// Failure to look is not evidence, the Watcher's rule. Here it is
		// also the safe direction: writing the flags word of an interface
		// whose current flags could not be read is how IFF_UP gets cleared.
		return false
	}
	on, err := HasAllMulti(flags)
	if err != nil || on {
		return false
	}
	if err := k.Set(); err != nil {
		k.failed++
		// Once. This runs on the 30s repair ticker, and the log relay
		// forwards "could not" to the controller — a line per tick would put
		// two an hour into somebody's Home Assistant log for ever.
		if !k.warned {
			k.warned = true
			log.Printf("[mcast] could not put %s into ALLMULTI: %v — inbound "+
				"multicast may stop arriving, which makes this device "+
				"invisible to Spotify Connect and AirPlay", k.Iface, err)
		}
		return false
	}
	k.sets++
	k.warned = false
	log.Printf("[mcast] %s put into ALLMULTI so inbound multicast arrives "+
		"(set #%d). The interface's own address filter had dropped the mDNS "+
		"group while the kernel still held the membership.", k.Iface, k.sets)
	return true
}

// Run ticks until done is closed. The first check is immediate: the flag is
// what makes the endpoints findable at all, and there is nothing to measure
// that a wait would make more accurate.
func (k *MultiKeeper) Run(done <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	k.Tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			k.Tick()
		}
	}
}
