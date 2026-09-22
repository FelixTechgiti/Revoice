/* ap2_ports.c — see ap2_ports.h.
 *
 * Pure: no sockets, no shairport-sync headers. Driven on the host by
 * ap2portscheck.c, the emos/init/pwcheck.c pattern.
 *
 * Parsed on every call rather than cached. It runs a handful of times per
 * AirPlay session, so the cost is nothing, and a cache would need either a
 * pthread_once or an argument about which threads reach bind_socket_and_port
 * — an argument that is only ever wrong once.
 */

#include "ap2_ports.h"

#include <errno.h>
#include <limits.h>
#include <stdlib.h>

/* strtol with the whole string consumed, because a trailing character is how a
 * typo becomes a plausible number: "6011x" must not read as 6011. */
static int parse_u16(const char *s, long *out) {
  if (s == NULL || *s == '\0')
    return -1;
  errno = 0;
  char *end = NULL;
  long v = strtol(s, &end, 10);
  if (errno != 0 || end == NULL || *end != '\0')
    return -1;
  *out = v;
  return 0;
}

int em_ap2_port_range(uint16_t *base, uint16_t *count) {
  long b = 0, c = 0;

  if (base == NULL || count == NULL)
    return -1;
  if (parse_u16(getenv(EM_AP2_PORT_BASE_ENV), &b) != 0)
    return -1;
  if (parse_u16(getenv(EM_AP2_PORT_COUNT_ENV), &c) != 0)
    return -1;

  if (b < 1024 || b > 65535)
    return -1;
  if (c < 1 || c > 256)
    return -1;
  if (b + c - 1 > 65535)
    return -1;

  *base = (uint16_t)b;
  *count = (uint16_t)c;
  return 0;
}
