package mcast

import (
	"encoding/binary"
	"log"
	"net"
	"time"
)

// This file sends the IGMP membership report the kernel will not, so the
// group is re-asserted before anything upstream ages it out.
//
// # What the Watcher and the Prober could not do
//
// The Watcher repairs a membership the KERNEL has lost; the Prober repairs an
// AP that has let the group go. Both repair by restarting the endpoints,
// because that is the only re-join reachable from outside the responders'
// own sockets — and both are therefore repairs after the fact, costing
// whatever was playing.
//
// Neither prevents it, and the prevention is cheap. Linux emits an
// unsolicited membership report only when a group's refcount on an interface
// goes 0 to 1. librespot and shairport-sync hold 224.0.0.251 for the life of
// the process, so after boot the membership is asserted exactly once, and
// nothing renews it: an AP that ages its snooping entry out stops forwarding,
// and the IGMP General Query that would prompt a fresh report is itself
// multicast, so the device can never hear the one message that would end the
// lock. That is the self-locking fault `Prober` already describes.
//
// # Measured, 2026-10-12, on Studio
//
// Both endpoints alive for 9.8 days, the membership present throughout, and
// `Udp: InDatagrams` from /proc/net/snmp frozen across a 20-second browse
// that six other Spotify hosts answered. Then, with nothing restarted and no
// socket touched, one v2 and one v3 report sent by hand:
//
//	852294..852299  frozen at 1414559      (six seconds, nothing arriving)
//	SEND            v2 -> group, v3 -> 224.0.0.22
//	852309          1414565                (+6, nine seconds later)
//	852313          1414568                (+3)
//
// and the controller's scan then read AirPlay as visible with its RTSP port
// reachable. So the report alone repairs it, and a report on a cadence inside
// the ageing interval means the lock never forms.
//
// # And one restart is not a re-join
//
// Measured the same session: killing librespot alone took the refcount 1 to 2
// rather than 0 to 1, emitted no report, and left the device exactly as
// invisible twenty seconds later. The existing repair restarts BOTH endpoints
// and works because they are down together — which is a property of how
// `Restart` kills and lets the supervisor re-exec, not something either
// caller states. Worth knowing before anybody makes that repair gentler.

// AllV3Routers is where an IGMPv3 report is addressed. A v2 report goes to
// the group itself; a v3 report goes here and names the group inside.
const AllV3Routers = "224.0.0.22"

// GroupMembershipInterval is how long a snooping switch keeps forwarding a
// group it has heard a report for: RFC 3376's robustness (2) times the query
// interval (125s) plus the maximum response time (10s). It is the number to
// stay inside, and it matches what was measured — reception on Studio lasted
// roughly four and a half minutes after a re-join.
const GroupMembershipInterval = 260 * time.Second

// DefaultReportEvery puts two reports inside every interval, which is the
// robustness the protocol assumes: one lost report must not cost the
// membership. Cheap enough that the margin is not worth trimming — two
// packets of 32 and 40 bytes.
const DefaultReportEvery = 120 * time.Second

// V2Report builds an IGMPv2 Membership Report for group, addressed to the
// group itself. Nil for anything that is not an IPv4 address, so a typo
// cannot be sent as a packet.
func V2Report(group net.IP) []byte {
	g := group.To4()
	if g == nil {
		return nil
	}
	m := make([]byte, 8)
	m[0] = 0x16 // Membership Report, version 2
	copy(m[4:], g)
	binary.BigEndian.PutUint16(m[2:], checksum(m))
	return ipHeader(g, m)
}

// V3Report builds an IGMPv3 Membership Report for group, addressed to
// AllV3Routers and carrying one group record.
//
// The record type is MODE_IS_EXCLUDE (2) — a CURRENT-STATE record, which is
// exactly what the kernel sends in answer to a General Query. A state-change
// type would claim a transition that is not happening, and a querier that
// believes it may act on the claim rather than on the state.
func V3Report(group net.IP) []byte {
	g := group.To4()
	if g == nil {
		return nil
	}
	m := make([]byte, 16)
	m[0] = 0x22 // Membership Report, version 3
	binary.BigEndian.PutUint16(m[6:], 1)
	m[8] = 2 // MODE_IS_EXCLUDE
	// m[9] auxiliary data length, m[10:12] number of sources: both zero. No
	// sources with EXCLUDE is an any-source membership, which is what a
	// responder joining 224.0.0.251 holds.
	copy(m[12:], g)
	binary.BigEndian.PutUint16(m[2:], checksum(m))
	return ipHeader(net.ParseIP(AllV3Routers).To4(), m)
}

// ipHeader wraps an IGMP message in an IP header carrying the Router Alert
// option (RFC 2113), which RFC 3376 requires on every IGMP message and which
// is what a snooping switch looks for. IHL is therefore 6 words, not 5.
//
// Three fields are left for the kernel, which fills them under IP_HDRINCL and
// would overwrite anything written here: the identifier, the header checksum
// and the source address. The source especially — taking the route's own
// address is what keeps this from disagreeing with the address the responders
// advertise.
func ipHeader(dst, payload []byte) []byte {
	o := make([]byte, 24+len(payload))
	o[0] = 0x46 // version 4, IHL 6
	o[1] = 0xC0 // DSCP CS6, as RFC 3376 asks for
	binary.BigEndian.PutUint16(o[2:], uint16(len(o)))
	o[8] = 1          // TTL 1 — IGMP never leaves the link
	o[9] = 2          // IPPROTO_IGMP
	copy(o[16:], dst) // 12:16 source: the kernel fills it
	o[20] = 0x94      // Router Alert, copied, option class 0, number 20
	o[21] = 0x04      // length, including these two bytes
	copy(o[24:], payload)
	return o
}

// checksum is the one-complement sum every IP-family checksum uses. Every
// IGMP message built here is an even number of bytes, so there is no odd tail.
func checksum(p []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(p); i += 2 {
		s += uint32(p[i])<<8 | uint32(p[i+1])
	}
	for s>>16 != 0 {
		s = s&0xFFFF + s>>16
	}
	return ^uint16(s)
}

// Reporter re-asserts the membership on a cadence, so the group is never aged
// out upstream.
//
// It sends rather than joins, and that is the same decision the Watcher
// documents from the other side: a membership held on a socket of ours would
// make /proc/net/igmp read healthy whatever the responders hold, blinding the
// instrument. A report asserts the membership that is already there and adds
// no second holder.
type Reporter struct {
	// Send writes one built packet to dst. Injected so the cadence, the gates
	// and the wording are host-testable without a raw socket.
	Send func(pkt []byte, dst net.IP) error
	// Active reports whether anything that should be a member is running. The
	// same gate the Watcher and the Prober use: a device with both endpoints
	// off has no membership to assert, and claiming one would ask the network
	// to forward a group nothing on this device reads.
	Active func() bool
	// Joined reports whether the kernel still holds the group. Without it
	// this would assert a membership that does not exist — true of a device
	// whose responders have both gone, where the honest answer is the
	// Watcher's repair and not a louder claim.
	Joined func() bool

	Group string
	Every time.Duration

	sent   int
	failed int
	warned bool
	next   time.Time
}

// Sent is how many report pairs have gone out, for a test and for the stats
// report: a device whose reports are failing can then be told from one whose
// reports are landing and are not being honoured.
func (r *Reporter) Sent() int { return r.sent }

// Failed is how many cadences could not send at all.
func (r *Reporter) Failed() int { return r.failed }

// Tick sends one report pair if it is due and the gates allow. Takes the
// clock so the cadence is testable without waiting it out.
func (r *Reporter) Tick(now time.Time) bool {
	if r.Send == nil {
		return false
	}
	if r.Group == "" {
		r.Group = Group
	}
	if r.Every <= 0 {
		r.Every = DefaultReportEvery
	}
	if r.Active != nil && !r.Active() {
		return false
	}
	if r.Joined != nil && !r.Joined() {
		return false
	}
	if !r.next.IsZero() && now.Before(r.next) {
		return false
	}
	r.next = now.Add(r.Every)

	g := net.ParseIP(r.Group)
	v2, v3 := V2Report(g), V3Report(g)
	if v2 == nil || v3 == nil {
		return false
	}
	err2 := r.Send(v2, g)
	err3 := r.Send(v3, net.ParseIP(AllV3Routers))
	if err2 != nil && err3 != nil {
		r.failed++
		// Once. This runs every two minutes for the life of the process, and
		// the log relay forwards "could not" to the controller — a line per
		// cadence would put thirty an hour into somebody's Home Assistant log
		// for a repair that has a backstop.
		if !r.warned {
			r.warned = true
			log.Printf("[mcast] could not send the membership report for %s: %v "+
				"— this device will go invisible to Spotify Connect and AirPlay "+
				"when the group is next aged out, and will be repaired by "+
				"restarting the endpoints instead", r.Group, err2)
		}
		return false
	}
	r.sent++
	r.warned = false
	return true
}

// Run reports until done is closed.
//
// The FIRST report fires at once rather than after a cadence, and that is the
// opposite of what the Prober does. The Prober waits because a probe into a
// booting device reports a fault that is really a boot; this has nothing to
// measure and nothing to get wrong, and a device whose endpoints have just
// started is exactly the moment the membership is freshest and cheapest to
// keep.
func (r *Reporter) Run(done <-chan struct{}, every time.Duration) {
	if every <= 0 {
		every = DefaultReportEvery
	}
	r.Every = every
	t := time.NewTicker(every)
	defer t.Stop()
	r.Tick(time.Now())
	for {
		select {
		case <-done:
			return
		case now := <-t.C:
			r.Tick(now)
		}
	}
}
