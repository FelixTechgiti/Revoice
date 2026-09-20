/*
 * txtcheck.c — the TXT length boundary, driven on a host.
 *
 *   cc -O2 -o txtcheck txtcheck.c && ./txtcheck
 *
 * tinysvc_txt.c is included whole — the pwcheck.c trick, the same one
 * mdnscheck.c and shmcheck.c use — so this drives the real function rather
 * than a copy of it.
 *
 * It exists because being wrong here was SILENT at the only end anyone
 * watches. tinysvcmdns answered NULL for a 67-byte string, stored it, and
 * dereferenced it on the responder thread; from outside, AirPlay 2 was a
 * receiver that restarted every minute with no message. The one number that
 * separates working from not is 63 against 255, so that is the number pinned
 * here — including 64, which is the first value the old code got wrong, and
 * 67, which is the length of the record that actually shipped.
 *
 * Exits non-zero on the first failure and says which. Run by CI beside
 * localhostcheck, mdnscheck, shmcheck and uuidcheck.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "tinysvc_txt.c"

static int failures = 0;

static void ok(int cond, const char *what) {
  printf("%-68s %s\n", what, cond ? "ok" : "FAIL");
  if (!cond)
    failures++;
}

/* A string of n 'x'es, NUL-terminated. */
static char *rep(size_t n) {
  char *s = malloc(n + 1);
  if (s == NULL) {
    fprintf(stderr, "out of memory\n");
    exit(2);
  }
  memset(s, 'x', n);
  s[n] = '\0';
  return s;
}

/* The shape the encoder reads: byte 0 is the length, the bytes follow, and a
 * NUL sits after them so its strncpy stops. */
static int encodes(const char *in) {
  size_t len = strlen(in);
  uint8_t *out = em_txt_label(in);
  int good;
  if (out == NULL)
    return 0;
  good = out[0] == (uint8_t)len && memcmp(out + 1, in, len) == 0 && out[len + 1] == '\0';
  free(out);
  return good;
}

int main(void) {
  char *s;

  /* The record that shipped, and the reason this file exists: `pk=` plus a
   * 32-byte Ed25519 public key as hex. */
  s = rep(64);
  char pk[80];
  snprintf(pk, sizeof(pk), "pk=%s", s);
  free(s);
  ok(strlen(pk) == 67, "AirPlay 2's pk= record is 67 bytes");
  ok(encodes(pk), "a 67-byte pk= record encodes rather than answering NULL");

  /* The old limit and the first value past it. 63 always worked, which is
   * why nothing shorter than AirPlay 2 ever found this. */
  s = rep(63);
  ok(encodes(s), "63 bytes, the old name-label limit, still encodes");
  free(s);
  s = rep(64);
  ok(encodes(s), "64 bytes, the first length the old code refused, encodes");
  free(s);

  /* The real limit, which is the length prefix being one byte. */
  s = rep(EM_TXT_MAX);
  ok(encodes(s), "255 bytes, the DNS-SD maximum, encodes");
  free(s);
  s = rep(EM_TXT_MAX + 1);
  ok(em_txt_label(s) == NULL, "256 bytes is refused rather than truncated");
  free(s);

  /* An empty TXT string is legal and is what a service with no attributes
   * advertises; the caller writes one into the record precisely so the
   * encoder never reads a NULL. */
  ok(encodes(""), "the empty string encodes as a zero length byte");

  ok(em_txt_label(NULL) == NULL, "NULL in is NULL out rather than a crash");

  /* A record with a NUL inside it would be truncated by the encoder's
   * strncpy, so the length byte must describe what will actually be sent.
   * strlen is what both agree on. */
  ok(encodes("flags=0x4"), "an ordinary record round-trips");

  if (failures) {
    printf("\n%d check(s) FAILED\n", failures);
    return 1;
  }
  printf("\nall checks passed\n");
  return 0;
}
