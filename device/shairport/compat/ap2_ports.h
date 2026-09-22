/*
 * ap2_ports.h — the port range AirPlay 2's per-session sockets are taken
 * from, so that a firewall rule can name them.
 *
 * # Why this exists
 *
 * For every AirPlay 2 session shairport-sync binds sockets on ports the
 * KERNEL chooses, tells the client their numbers in the SETUP response, and
 * waits for the client to connect IN. Read off rtsp.c and unchanged between
 * 4.3.7 and 5.5.1, so it is not something a version bump fixes:
 *
 *     conn->local_event_port = 0;          // any port  (TCP, twice)
 *     conn->local_ap2_control_port = 0;    // any port  (UDP)
 *     conn->local_buffered_audio_port = 0; // any port  (TCP)
 *
 * On a device with a default-DROP INPUT policy — which is every device this
 * project runs on, FireOS by Amazon's design and emOS by ours — those inbound
 * connections are dropped. The session negotiates, the client is told a port,
 * and nothing arrives. It presents as a speaker that appears, accepts a
 * connection and plays nothing, rather than as a firewall.
 *
 * A firewall rule cannot name a number that does not exist yet, so the range
 * is pinned here and the same constants are read by internal/netfilter. One
 * definition, so the rule and the listener cannot drift — the same shape the
 * RTSP and RTP ports already use.
 *
 * # Why the environment and not a config key
 *
 * shairport-sync's config parser would have to learn two keys, in a file we
 * otherwise do not patch at all. The environment is how this project already
 * hands a shim its one answer (REVOICE_SHM_DIR), the firmware already sets
 * the child's environment explicitly, and an unset variable is a build that
 * behaves exactly as upstream does — which is the property that matters, since
 * this same source is what a classic build compiles.
 *
 * # What an unset or unusable value means
 *
 * "Ask the kernel", i.e. upstream's behaviour. Never "fail": a receiver that
 * refuses to start because a range was mistyped is worse than one that plays
 * for somebody whose firewall is open, and the alternative failure is loud in
 * the log rather than silent on the network.
 */

#ifndef REVOICE_AP2_PORTS_H
#define REVOICE_AP2_PORTS_H

#include <stdint.h>

#define EM_AP2_PORT_BASE_ENV "REVOICE_AP2_PORT_BASE"
#define EM_AP2_PORT_COUNT_ENV "REVOICE_AP2_PORT_COUNT"

/* Resolve the range. 0 and both values written when one is configured and
 * usable; -1 when it is unset or unusable, and then the caller must fall back
 * to letting the kernel pick.
 *
 * Refuses a base below 1024 (binding a privileged port for a music session is
 * not something a typo should be able to ask for), a count of 0, and any range
 * that would run past 65535. */
int em_ap2_port_range(uint16_t *base, uint16_t *count);

#endif /* REVOICE_AP2_PORTS_H */
