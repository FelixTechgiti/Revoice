/* Prove the getaddrinfo shim on a host, because the two programs that need it
 * both die on the first call and say so in a message naming the wrong function.
 *
 * android_localhost.c is included whole — the pwcheck.c trick, as shmcheck.c,
 * mdnscheck.c and uuidcheck.c do — so this drives the real function rather than
 * a copy.
 *
 *   cc -O2 -o localhostcheck localhostcheck.c && ./localhostcheck
 *
 * What it can and cannot answer, stated because the gap is the interesting
 * part: the host HAS a resolver that answers `localhost`, so this cannot
 * reproduce the device's failure and does not try. What it checks is the two
 * properties that decide whether the shim is right once the rewrite happens —
 * which names are rewritten, and that the result is a single IPv4 address the
 * two programs cannot disagree about. Both are silent on hardware: a name left
 * out is a binary that exits once a minute for ever, and an IPv6 result in
 * front of an IPv4 one is a control message sent where nobody is listening.
 *
 * Exits non-zero on the first failure and says which. Run by CI beside
 * shmcheck, mdnscheck, uuidcheck, ringsim --check and pwcheck.
 */
#include <netinet/in.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>

#include "android_localhost.c"

static int failures = 0;

static void ok(int cond, const char *what) {
  printf("%-68s %s\n", what, cond ? "ok" : "FAIL");
  if (!cond)
    failures++;
}

/* Exactly the hints both callers pass: ptp-utilities.c:230 and
 * nqptp-utilities.c:56 are the same four lines, so a change in what the shim
 * does to them shows up here rather than on a device. */
static void their_hints(struct addrinfo *hints) {
  memset(hints, 0, sizeof *hints);
  hints->ai_family = AF_UNSPEC;
  hints->ai_socktype = SOCK_DGRAM;
  hints->ai_flags = AI_PASSIVE;
}

int main(void) {
  struct addrinfo hints, *info = NULL;
  int ret;

  /* ── which names ─────────────────────────────────────────────────────── */

  ok(em_is_loopback_name("localhost"), "localhost is rewritten");
  ok(em_is_loopback_name("LocalHost"),
     "the match ignores case, as a hosts file lookup does");
  ok(em_is_loopback_name("localhost.localdomain"),
     "localhost.localdomain is rewritten");

  ok(!em_is_loopback_name(NULL),
     "a NULL node is left alone: that is the wildcard bind, not a name");
  ok(!em_is_loopback_name("127.0.0.1"),
     "a literal is left alone rather than rewritten to itself");
  ok(!em_is_loopback_name("localhost6"),
     "localhost6 means the IPv6 loopback and is NOT answered with IPv4");
  ok(!em_is_loopback_name("ip6-localhost"),
     "ip6-localhost means the IPv6 loopback and is NOT answered with IPv4");
  ok(!em_is_loopback_name("localhostess.example.com"),
     "a name merely starting with localhost is a real name");
  ok(!em_is_loopback_name(""),
     "the empty node is left alone");

  /* ── what comes back ─────────────────────────────────────────────────── */

  their_hints(&hints);
  ret = em_getaddrinfo("localhost", "9000", &hints, &info);
  ok(ret == 0 && info != NULL, "localhost resolves through the shim");

  if (ret == 0 && info != NULL) {
    int v4_first = info->ai_family == AF_INET;
    int any_v6 = 0;
    int loopback_first = 0;
    int n = 0;

    if (v4_first) {
      const struct sockaddr_in *sin = (const struct sockaddr_in *)info->ai_addr;
      loopback_first = sin->sin_addr.s_addr == htonl(INADDR_LOOPBACK) &&
                       sin->sin_port == htons(9000);
    }
    for (struct addrinfo *p = info; p != NULL; p = p->ai_next) {
      n++;
      if (p->ai_family == AF_INET6)
        any_v6 = 1;
    }

    /* The FIRST result is the whole of what shairport-sync uses, and nqptp
     * binds the rest — so "IPv4 first" is not a preference, it is what keeps
     * the sender and the binder on one address. */
    ok(v4_first, "the first result is IPv4");
    ok(loopback_first, "the first result is 127.0.0.1 at the port asked for");
    ok(!any_v6, "no IPv6 result is offered at all");
    ok(n >= 1, "at least one result");

    /* Upstream frees this with the real freeaddrinfo, which is only safe
     * because the shim never allocated it. */
    freeaddrinfo(info);
  }

  /* A name that is not loopback still reaches the real resolver. Checked
   * against a literal rather than a hostname, so the test needs no DNS: CI
   * runners have resolvers, but a check that fails when a network does is a
   * check people learn to re-run rather than read. */
  info = NULL;
  their_hints(&hints);
  ret = em_getaddrinfo("192.0.2.7", "9000", &hints, &info);
  ok(ret == 0 && info != NULL, "a non-loopback node is passed straight through");
  if (ret == 0 && info != NULL)
    freeaddrinfo(info);

  if (failures) {
    printf("\n%d check(s) failed\n", failures);
    return 1;
  }
  printf("\nall checks passed\n");
  return 0;
}
