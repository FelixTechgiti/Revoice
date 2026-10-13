/*
 * ifaddrscheck.c — the AirPlay identity must come from the radio, and must
 * be the same after a reboot.
 *
 * This drives the real functions by #including android_ifaddrs.c whole, the
 * way shmcheck.c does, because the property being checked is behaviour and
 * the only other verdict on this file is a rooted Echo.
 *
 * What it is for: shairport-sync takes the MAC of the first AF_PACKET entry
 * that carries one, and that MAC becomes the AirPlay 2 device ID, the
 * `_raop._tcp` instance name and the identity HomeKit pairs against. Netlink
 * dumps links in interface-index order, which on this board puts two
 * traffic-shaping dummies in front of the radio — and their addresses are
 * generated at random when the module loads. The failure is silent and
 * total: everything works, and the speaker somebody added to Apple Home is
 * a different speaker after every reboot.
 */

#define __ANDROID__ 1

#include <stdio.h>
#include <string.h>

#include "android_ifaddrs.c"

static int failures;

static void ok(int cond, const char *what) {
  if (!cond) {
    failures++;
    printf("FAIL  %s\n", what);
  } else {
    printf("ok    %s\n", what);
  }
}

/* The interface table as it was read off Studio on 2026-10-13, in the index
 * order netlink reports. The addresses of ifb0 and ifb1 are what the kernel
 * happened to generate on that boot; the point of the test is that they must
 * not be chosen whatever they are. */
struct sample {
  const char *name;
  unsigned int flags;
  unsigned char hw[6];
  int hwlen;
};

static const struct sample studio[] = {
    {"lo", 0x9, {0, 0, 0, 0, 0, 0}, 6},
    {"ifb0", 0x82, {0x8e, 0xc2, 0x01, 0xf2, 0xb2, 0xc5}, 6},
    {"ifb1", 0x82, {0x1a, 0x41, 0xfd, 0x78, 0x5f, 0x51}, 6},
    {"tunl0", 0x80, {0, 0, 0, 0, 0, 0}, 4},
    {"wlan0", 0x1203, {0xfc, 0xa1, 0x83, 0xc6, 0x93, 0x15}, 6},
};

/* Build the list the way em_handle_link does, without netlink. */
static struct ifaddrs *build(struct em_ctx *ctx, const struct sample *in,
                             int n) {
  int i;
  memset(ctx, 0, sizeof(*ctx));
  for (i = 0; i < n; i++) {
    struct em_node *node;
    struct sockaddr_ll *sll;
    int primary = em_link_is_primary(in[i].flags, in[i].hw, in[i].hwlen);
    node = em_node_new(ctx, in[i].name, in[i].flags, primary);
    if (!node)
      return NULL;
    sll = (struct sockaddr_ll *)&node->addr;
    sll->sll_family = AF_PACKET;
    sll->sll_halen = (unsigned char)in[i].hwlen;
    memcpy(sll->sll_addr, in[i].hw, (size_t)in[i].hwlen);
    node->pub.ifa_addr = (struct sockaddr *)&node->addr;
  }
  return ctx->head;
}

/* What common.c does: the first AF_PACKET entry with a hardware address. */
static const char *first_mac_owner(struct ifaddrs *head, char *mac,
                                   size_t cap) {
  struct ifaddrs *p;
  for (p = head; p; p = p->ifa_next) {
    struct sockaddr_ll *sll = (struct sockaddr_ll *)p->ifa_addr;
    if (!sll || sll->sll_family != AF_PACKET || sll->sll_halen == 0)
      continue;
    snprintf(mac, cap, "%02x:%02x:%02x:%02x:%02x:%02x", sll->sll_addr[0],
             sll->sll_addr[1], sll->sll_addr[2], sll->sll_addr[3],
             sll->sll_addr[4], sll->sll_addr[5]);
    return p->ifa_name;
  }
  return NULL;
}

int main(void) {
  struct em_ctx ctx;
  struct ifaddrs *head;
  char mac[32];
  const char *owner;
  int count;
  struct ifaddrs *p;

  /* The predicate, on the measured table. */
  ok(em_link_is_primary(0x1203, studio[4].hw, 6) == 1, "wlan0 is primary");
  ok(em_link_is_primary(0x82, studio[1].hw, 6) == 0,
     "ifb0 is not primary: it is DOWN");
  ok(em_link_is_primary(0x9, studio[0].hw, 6) == 0,
     "lo is not primary: its address is all zeros");
  ok(em_link_is_primary(0x1203, NULL, 0) == 0, "no address is not primary");
  ok(em_link_is_primary(0x1203, studio[4].hw, 0) == 0,
     "a zero-length address is not primary");

  /* And the ordering it produces. */
  head = build(&ctx, studio, 5);
  ok(head != NULL, "the list was built");
  if (!head)
    return 1;

  owner = first_mac_owner(head, mac, sizeof(mac));
  ok(owner != NULL && strcmp(owner, "wlan0") == 0,
     "the first hardware address belongs to the radio");
  ok(strcmp(mac, "fc:a1:83:c6:93:15") == 0,
     "and it is the radio's own address, not a dummy's");
  if (owner)
    printf("      identity would be %s from %s\n", mac, owner);

  /* Nothing may be lost by the reordering: every interface is still there,
   * and the tail still ends the list. */
  count = 0;
  for (p = head; p; p = p->ifa_next)
    count++;
  ok(count == 5, "every interface is still in the list");
  ok(ctx.tail != NULL && ctx.tail->ifa_next == NULL,
     "the tail is the last entry");

  /* The whole point: a different random address on the dummies must not
   * change the answer, because that is what a reboot does. */
  {
    struct sample next_boot[5];
    struct em_ctx ctx2;
    char mac2[32];
    const char *owner2;
    memcpy(next_boot, studio, sizeof(next_boot));
    next_boot[1].hw[0] = 0xee;
    next_boot[1].hw[1] = 0x98;
    next_boot[1].hw[2] = 0x90;
    next_boot[2].hw[0] = 0x4a;
    head = build(&ctx2, next_boot, 5);
    owner2 = head ? first_mac_owner(head, mac2, sizeof(mac2)) : NULL;
    ok(owner2 != NULL && strcmp(mac2, mac) == 0,
       "the identity survives a reboot that re-rolls the dummies");
  }

  /* A board with no primary interface at all must still produce a list in
   * the order it was given, rather than nothing. */
  {
    struct em_ctx ctx3;
    struct ifaddrs *h3 = build(&ctx3, studio, 4); /* without wlan0 */
    ok(h3 != NULL && strcmp(h3->ifa_name, "lo") == 0,
       "with no primary interface the kernel's order is kept");
  }

  if (failures) {
    printf("\n%d check(s) failed\n", failures);
    return 1;
  }
  printf("\nall checks passed\n");
  return 0;
}
