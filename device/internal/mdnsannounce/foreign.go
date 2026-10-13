package mdnsannounce

// ForeignCount reports how many mDNS packets from OTHER hosts have reached this
// device, and whether there is a socket to ask at all.
//
// # Why the count belongs here and not on the firewall
//
// The deafness probe used to read the packet counter on the firewall's own
// mDNS ACCEPT rule, which matches on the DESTINATION address and says nothing
// about where a packet came from. The kernel delivers a locally sent multicast
// packet back up the input path with the outgoing interface still on it, so
// this device's own announcements — and the announcements its endpoints make
// when something restarts them — traverse that chain and move that counter.
//
// The probe's repair restarted both endpoints, both announced themselves as
// they came up, and the loopback copies landed on the counter the probe was
// about to read. So the repair manufactured the recovery it then reported, and
// did it once a minute after every attempt, for ever. Measured on Studio,
// 2026-10-13, with the arrivals split by source in `mangle PREROUTING`: two
// consecutive repairs each logged "hears the network again" over exactly the
// two packets this device had just sent, with **nothing** from any other host
// across the whole 22 minutes (#384).
//
// This socket already knows the difference, because `Watch` has to: it caches
// only what this device published and discards the rest. The packets it
// discards are the measurement, so the instrument costs one counter on a
// socket that is already open — no exec, no positional parsing of iptables
// output, and no new rule on a firewall whose device is managed over the
// network it protects.
//
// Three properties a caller depends on:
//
//   - It only ever grows, within one process. A restart starts from zero,
//     which the tracker already reads as a re-baseline rather than as silence.
//   - A packet whose origin could not be decided is counted as neither.
//     Failure to look is not evidence — the rule the membership watcher and
//     the wake-word reconcile both follow.
//   - No listener is not a count of zero. `ok` is false, and a caller must
//     report nothing rather than report the device deaf: a socket that never
//     opened is one this device cannot be asked about.
func (l *Listener) ForeignCount() (int64, bool) {
	if l == nil || l.conn == nil {
		return 0, false
	}
	return l.foreign.Load(), true
}
