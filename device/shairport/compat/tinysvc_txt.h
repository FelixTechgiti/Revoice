/*
 * tinysvc_txt.h — one DNS-SD TXT string, encoded the way tinysvcmdns's
 * packet writer reads it.
 *
 * The rule this exists to state: a TXT string may be 255 bytes, a NAME label
 * may be 63, and they are not the same limit. tinysvcmdns has one helper for
 * both (`create_label`), which is why AirPlay 2 could never be advertised
 * from this device — see tinysvc_txt.c.
 *
 * No tinysvcmdns header is needed to use this, deliberately: it takes a C
 * string and returns bytes, so txtcheck.c can drive the real function on a
 * host with none of shairport-sync's build tree present.
 */
#ifndef EM_TINYSVC_TXT_H
#define EM_TINYSVC_TXT_H

#include <stdint.h>

/* The longest a single DNS-SD TXT string may be (RFC 6763 section 6.1): the
 * length prefix is one byte. NOT 63 — that is the name-label limit. */
#define EM_TXT_MAX 255

/* Encodes `s` as <length byte><bytes><NUL> in one malloc'd block, which is
 * the shape tinysvcmdns's encoder expects: it reads the length out of byte 0
 * and copies length+1 bytes with strncpy, so the trailing NUL is what stops
 * that copy running on.
 *
 * Returns NULL, and only NULL, for a string that cannot be represented: a
 * NULL argument, one longer than EM_TXT_MAX, or a failed malloc. A caller
 * must check — storing the NULL is precisely the upstream bug. */
uint8_t *em_txt_label(const char *s);

#endif /* EM_TINYSVC_TXT_H */
