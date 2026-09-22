
/* ---------------------------------------------------------------------------
 * Appended to shairport-sync's common.c by device/shairport/ap2/in-container.sh.
 *
 * The definition above was renamed to em_bind_socket_and_port_once by the same
 * step; this is the bind_socket_and_port everything else in the tree calls, so
 * common.h needs no change and neither does any call site.
 *
 * Why it is here at all: every AirPlay 2 session asks for "any port" four times
 * and tells the client the numbers, and on a default-DROP device the client's
 * inbound connections to those ports are dropped. See compat/ap2_ports.h.
 *
 * With no range configured this is one extra comparison and upstream's
 * behaviour, which is what a classic build and anybody running the binary by
 * hand get.
 *
 * Note the retry loop inherits em_bind_socket_and_port_once's own warn() on a
 * failed bind, so a busy port logs a line before the next one is tried. Left
 * that way: suppressing it would mean editing the function this patch exists
 * to leave alone, and a port that is busy every session is worth seeing.
 * ------------------------------------------------------------------------- */

#include "ap2_ports.h"

int bind_socket_and_port(int type, int ip_family, const char *self_ip_address, uint32_t scope_id,
                         uint16_t *port, int *sock) {
  uint16_t base = 0, count = 0;

  /* A caller that named a port gets that port. Only "any" is ours to place —
   * the RTSP listener and the classic RTP ports are already pinned elsewhere
   * and must not be moved into this range. */
  if (port == NULL || *port != 0 || em_ap2_port_range(&base, &count) != 0)
    return em_bind_socket_and_port_once(type, ip_family, self_ip_address, scope_id, port, sock);

  for (uint16_t i = 0; i < count; i++) {
    uint16_t candidate = (uint16_t)(base + i);
    if (em_bind_socket_and_port_once(type, ip_family, self_ip_address, scope_id, &candidate,
                                     sock) == 0) {
      *port = candidate;
      return 0;
    }
  }

  /* Exhausted. Falling back to a kernel-chosen port is the survivable answer:
   * the session may still work — the range is a firewall's problem, not the
   * protocol's — whereas refusing would take the receiver down over a full
   * range. Loud, because the next symptom is a session that negotiates and
   * plays nothing, and that is unreadable without this line. */
  warn("revoice: no free port in %u..%u for an AirPlay 2 session socket, so the kernel will "
       "pick one. It will not be a port the firewall names, so this session may negotiate and "
       "then carry no audio.",
       (unsigned)base, (unsigned)(base + count - 1));
  *port = 0;
  return em_bind_socket_and_port_once(type, ip_family, self_ip_address, scope_id, port, sock);
}
