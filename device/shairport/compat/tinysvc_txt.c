/*
 * tinysvc_txt.c — the TXT string encoder tinysvcmdns should have had.
 *
 * # Why this file exists
 *
 * tinysvcmdns builds every TXT string with `create_label`, the helper that
 * also builds DNS *name* labels — so it enforces the name-label limit of 63
 * bytes and returns NULL above it. `rr_add_txt` stores that NULL without
 * looking, and `mdns_encode_rr`'s RR_TXT case then reads `txt_rec->txt[0]`.
 *
 * AirPlay 2 advertises `pk=` plus a 32-byte Ed25519 public key as hex: 67
 * bytes. So the responder thread dereferenced NULL on its very first
 * announcement and the whole receiver died about a second after start, every
 * time, on every device. A DNS-SD TXT string is allowed 255 bytes; only the
 * name label is 63, and one helper for both is where the two got confused.
 *
 * Nothing upstream is patched for this. mdns_tinysvcmdns.c — which this fork
 * already replaces wholesale — builds the service's TXT record itself from
 * these bytes and hands it over with `mdnsd_add_rr`, so the 4.3.7 pin stays
 * as movable as it was.
 *
 * # Why it is its own file rather than a static helper next door
 *
 * So that it can be tested. mdns_tinysvcmdns.c needs shairport-sync's build
 * tree to compile at all, which is why `mdnscheck` covers the bookkeeping in
 * mdns_ap2.c and says in its own header that the backend is out of reach.
 * This file needs nothing but <stdlib.h>, so txtcheck.c can include it whole
 * and drive the real function at the boundary that bit us.
 */

#include "tinysvc_txt.h"

#include <stdlib.h>
#include <string.h>

uint8_t *em_txt_label(const char *s) {
  size_t len;
  uint8_t *out;

  if (s == NULL)
    return NULL;

  len = strlen(s);
  if (len > EM_TXT_MAX)
    return NULL;

  /* length byte + the bytes + a NUL. The NUL is not decoration: the encoder
   * copies with strncpy, and without it the copy reads past the record. */
  out = malloc(len + 2);
  if (out == NULL)
    return NULL;

  out[0] = (uint8_t)len;
  memcpy(out + 1, s, len);
  out[len + 1] = '\0';
  return out;
}
