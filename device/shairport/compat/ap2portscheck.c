/* Prove the AirPlay 2 port range is parsed the way a firewall rule assumes,
 * on a host, before any of it reaches a device.
 *
 * ap2_ports.c is included whole — the emos/init/pwcheck.c trick, the same one
 * shmcheck.c and mdnscheck.c use — so this drives the real function.
 *
 *   cc -O2 -o ap2portscheck ap2portscheck.c && ./ap2portscheck
 *
 * What makes it worth having: every rejection here falls back to upstream's
 * "ask the kernel", which on a default-DROP device is a session that
 * negotiates and then plays nothing. So a value wrongly rejected and a value
 * wrongly accepted look the same from the outside — silence — and the only
 * place the difference is visible is here.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "ap2_ports.c"

static int failures = 0;

static void ok(int cond, const char *what) {
  printf("%-62s %s\n", what, cond ? "ok" : "FAIL");
  if (!cond)
    failures++;
}

static void set(const char *b, const char *c) {
  if (b)
    setenv(EM_AP2_PORT_BASE_ENV, b, 1);
  else
    unsetenv(EM_AP2_PORT_BASE_ENV);
  if (c)
    setenv(EM_AP2_PORT_COUNT_ENV, c, 1);
  else
    unsetenv(EM_AP2_PORT_COUNT_ENV);
}

/* Resolves, and to exactly these numbers. */
static int is(const char *b, const char *c, int wb, int wc) {
  uint16_t base = 0, count = 0;
  set(b, c);
  return em_ap2_port_range(&base, &count) == 0 && base == wb && count == wc;
}

/* Does not resolve, so the caller asks the kernel — upstream's behaviour. */
static int no(const char *b, const char *c) {
  uint16_t base = 0, count = 0;
  set(b, c);
  return em_ap2_port_range(&base, &count) == -1;
}

int main(void) {
  ok(is("6011", "10", 6011, 10), "the ordinary case resolves");
  ok(is("1024", "1", 1024, 1), "the lowest permitted base, and a range of one");
  ok(is("65535", "1", 65535, 1), "the last port on its own");
  ok(is("65526", "10", 65526, 10), "a range ending exactly at 65535");

  /* Unset is the case that matters most: it is what a classic build and any
   * host running this binary by hand will see, and it must be upstream. */
  ok(no(NULL, NULL), "both unset -> ask the kernel");
  ok(no("6011", NULL), "a base with no count is not half a range");
  ok(no(NULL, "10"), "a count with no base is not half a range");
  ok(no("", ""), "empty is not zero");

  ok(no("1023", "10"), "a privileged base is refused");
  ok(no("0", "10"), "port zero is refused, not read as \"any\"");
  ok(no("-1", "10"), "a negative base is refused");
  ok(no("65536", "1"), "a base past the last port is refused");
  ok(no("6011", "0"), "a count of zero is refused");
  ok(no("6011", "257"), "an absurd count is refused");
  ok(no("65535", "2"), "a range running past 65535 is refused");

  /* A trailing character is how a typo becomes a plausible number. */
  ok(no("6011x", "10"), "trailing junk on the base is refused");
  ok(no("6011", "10 "), "trailing space on the count is refused");
  ok(no("six", "10"), "a non-number is refused");
  ok(no("0x1770", "10"), "hex is not accepted, because the rule is written in decimal");

  uint16_t b = 0;
  set("6011", "10");
  ok(em_ap2_port_range(&b, NULL) == -1 && em_ap2_port_range(NULL, &b) == -1,
     "a NULL out-parameter is refused rather than half-written");

  printf("\n%s\n", failures ? "FAILURES" : "all checks passed");
  return failures ? 1 : 0;
}
