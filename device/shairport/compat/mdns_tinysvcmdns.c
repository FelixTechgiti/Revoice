/*
 * mdns_tinysvcmdns.c — Revoice's replacement for shairport-sync's bundled
 * tinysvcmdns backend, so that AirPlay 2 can be discovered.
 *
 * This file REPLACES the upstream file of the same name at build time
 * (build.sh copies it over the checkout). It is not a patch, deliberately:
 * `--with-tinysvcmdns` already compiles this filename, so nothing in
 * configure.ac, Makefile.am or mdns.c has to be touched and the 4.3.7 pin
 * stays as movable as it was. A patch against upstream's body would conflict
 * on every bump; a whole file we own does not. The one thing that CAN drift
 * under us is the `mdns_backend` struct's shape — and that is a compile
 * error, not a silent one.
 *
 * # Why it exists
 *
 * AirPlay 2 is advertised as TWO services: `_raop._tcp` exactly as classic
 * AirPlay, and `_airplay._tcp` carrying the features, the public key and the
 * group state. Upstream's backend takes `ap2name` and `secondary_txt_records`
 * and declares both `__attribute__((unused))`, and sets no `mdns_update` at
 * all. So a build configured `--with-airplay-2 --with-tinysvcmdns`
 * configures, compiles, links and RUNS — and is never once offered to a
 * phone as an AirPlay 2 device, with nothing logged at either end. Only
 * `mdns_avahi.c` implements the second service upstream, which is where the
 * "AirPlay 2 requires Avahi" belief comes from; configure.ac enforces no such
 * thing. See #79.
 *
 * # Why an update tears the responder down and starts it again
 *
 * This is the part worth reading before "optimising" it.
 *
 * `rtsp.c` calls `mdns_update(NULL, secondary_txt_records)` at four points,
 * and every one of them is a change to the AirPlay 2 group state: `flags`
 * gains and loses DeviceSupportsRelay on SETUP and TEARDOWN, and `gid` /
 * `gcgl` / `isGroupLeader` change as the speaker joins and leaves a group.
 *
 * The obvious implementation is to find the TXT record and edit it in place.
 * tinysvcmdns even makes that *look* available: `struct rr_entry` is a
 * complete type in the public header and `rr_add_txt` is exported. It is
 * still wrong, for a reason that has nothing to do with the data race
 * (`struct mdnsd` is opaque, so its `data_lock` is out of reach anyway):
 * **tinysvcmdns announces on registration and at no other time, and its
 * records carry a 4500-second TTL.** A record edited in place is a record
 * every client on the network goes on ignoring for up to seventy-five
 * minutes, because it already has the old copy and no reason to ask again.
 * The edit would appear to work, in the sense that the responder's answers
 * would be correct, and grouping would still be broken.
 *
 * Re-announcing is the thing that has to happen, and `mdnsd_start()` is the
 * only code here that announces. So an update is a restart. It costs the
 * responder thread a stop and a start — bounded by mdnsd_stop's 500ms poll —
 * at a moment that is already a session boundary.
 *
 * # Both services' TXT records come from the caller
 *
 * `_raop._tcp` is registered from the `txt_records` argument and
 * `_airplay._tcp` from `secondary_txt_records`, with mdns.h's MDNS_RECORD_*
 * macros kept only as the fallback for a caller that passes none.
 *
 * **This paragraph said the opposite until #310, and the reason it gave was
 * checkable and wrong.** It claimed the macros carry `am=`, `vs=`, `sf=`,
 * `fv=` and `tp=` while `build_bonjour_strings` does not put them in the
 * array, so that using the argument would quietly change what classic AirPlay
 * advertises. `build_bonjour_strings` puts all five in the array, in both of
 * its branches, and upstream's own comment on the classic one says what it is
 * for:
 *
 *     #else
 *       // here, just replicate what happens in mdns.h when using those #defines
 *
 * So for a classic build the argument and the macros are the same strings by
 * construction and this changes nothing. For an AirPlay 2 build they are not:
 * the argument carries `ft=`, `pk=`, `tp=UDP` and `vs=366.0`, and the macros
 * carry `vs=105.1`, `tp=TCP,UDP`, `txtvers=1` and `ek=1` — so a device
 * advertised `_airplay._tcp` with a public key and `_raop._tcp` as an AirPlay
 * 1 receiver, was offered by every client, and could negotiate no session.
 * Measured on the air 2026-09-22.
 *
 * The caution was real and aimed at the wrong risk. What it was protecting —
 * that this file adds the second service rather than renegotiating the first
 * — is still true, and is now true because the first service says what
 * shairport-sync says rather than what this file decides.
 */

#include <ifaddrs.h>
#include <net/if.h>
#include <netinet/in.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#include "common.h"
#include "mdns.h"

#include "mdns_ap2.h"
#include "tinysvc_txt.h"
#include "tinysvcmdns.h"

static struct mdnsd *svr = NULL;

/* What is on the air. Held across an update because shairport resends only the
 * secondary half, and because a restart has to rebuild the whole
 * advertisement from something. */
static struct em_ad ad;

/* register / update / unregister are called from different threads — the
 * first from the main thread at startup, the others from RTSP connection
 * threads — and an update frees and replaces `svr`. Without this, a TEARDOWN
 * racing a SETUP can stop the responder twice. */
static pthread_mutex_t ad_lock = PTHREAD_MUTEX_INITIALIZER;

/* Give the responder this host's name and every non-loopback address it has.
 * Lifted from upstream's version of this file, with its two leaks fixed: it
 * returned on failure without freeing the list, and its final freeifaddrs()
 * was handed the loop variable, which is NULL by then. Ours frees the head.
 */
static int set_hostname_and_addresses(void) {
  char hostname[128];
  char withlocal[160];
  struct ifaddrs *ifalist = NULL, *ifa = NULL, *primary = NULL;

  if (gethostname(hostname, sizeof(hostname) - 1) != 0) {
    warn("tinysvcmdns: gethostname() failed");
    return -1;
  }
  hostname[sizeof(hostname) - 1] = 0; /* POSIX permits truncation with no NUL */

  if (em_hostname_local(hostname, withlocal, sizeof(withlocal)) != 0) {
    warn("tinysvcmdns: cannot form a .local hostname from \"%s\"", hostname);
    return -1;
  }

  if (getifaddrs(&ifalist) < 0) {
    warn("tinysvcmdns: getifaddrs() failed");
    return -1;
  }

  /* The first non-loopback address becomes the responder's own; the rest are
   * added as extra A/AAAA records for the same name. */
  for (ifa = ifalist; ifa != NULL; ifa = ifa->ifa_next) {
    if (config.interface != NULL && strcmp(config.interface, ifa->ifa_name) != 0)
      continue;
    if ((ifa->ifa_flags & IFF_LOOPBACK) || ifa->ifa_addr == NULL)
      continue;

    if (ifa->ifa_addr->sa_family == AF_INET) {
      uint32_t ip = ((struct sockaddr_in *)ifa->ifa_addr)->sin_addr.s_addr;
      mdnsd_set_hostname(svr, withlocal, ip);
      primary = ifa;
      break;
    } else if (ifa->ifa_addr->sa_family == AF_INET6) {
      struct in6_addr *a6 = &((struct sockaddr_in6 *)ifa->ifa_addr)->sin6_addr;
      mdnsd_set_hostname_v6(svr, withlocal, a6);
      primary = ifa;
      break;
    }
  }

  if (primary == NULL) {
    warn("tinysvcmdns: no non-loopback ipv4 or ipv6 interface found");
    freeifaddrs(ifalist);
    return -1;
  }

  for (ifa = primary->ifa_next; ifa != NULL; ifa = ifa->ifa_next) {
    if (ifa->ifa_addr == NULL || (ifa->ifa_flags & IFF_LOOPBACK))
      continue;
    if (config.interface != NULL && strcmp(config.interface, ifa->ifa_name) != 0)
      continue;

    switch (ifa->ifa_addr->sa_family) {
    case AF_INET: {
      uint32_t ip = ((struct sockaddr_in *)ifa->ifa_addr)->sin_addr.s_addr;
      mdnsd_add_rr(svr, rr_create_a(create_nlabel(withlocal), ip));
    } break;
    case AF_INET6: {
      struct in6_addr *a6 = &((struct sockaddr_in6 *)ifa->ifa_addr)->sin6_addr;
      mdnsd_add_rr(svr, rr_create_aaaa(create_nlabel(withlocal), a6));
    } break;
    default:
      break;
    }
  }

  freeifaddrs(ifalist);
  return 0;
}

/* Build the service's TXT record and give it to the responder.
 *
 * This is the one job deliberately taken away from mdnsd_register_svc, and
 * the reason is #229. Its TXT path goes through `rr_add_txt`, which encodes
 * each string with `create_label` — the helper for DNS *name* labels, which
 * refuses anything over 63 bytes and answers NULL. `rr_add_txt` stores that
 * NULL, and the responder thread dereferences it the first time it encodes
 * an announcement, which kills the whole receiver about a second after it
 * starts. AirPlay 2's `pk=` record is `pk=` plus a 32-byte key as hex: 67
 * bytes, every time, on every device — so AirPlay 2 could not once have
 * worked, and the crash is in a thread whose output nobody reads.
 *
 * A TXT string's real limit is 255 (RFC 6763 section 6.1). em_txt_label
 * enforces that one and returns NULL only for a string that genuinely cannot
 * be represented, which is checked here rather than stored.
 *
 * Everything below is the public tinysvcmdns API, so upstream stays
 * untouched: `struct mdnsd` is opaque, but `mdnsd_add_rr` takes its lock and
 * adds to the same group `mdnsd_register_svc` would have. The record's name
 * is built exactly as it builds it, or the SRV would point at one name and
 * the TXT would sit under another, and the two would never be answered
 * together.
 *
 * Caller holds ad_lock. */
static int add_txt_record(const char *instance, const char *type, const char *txt[]) {
  uint8_t *inst_label = create_label(instance);
  uint8_t *type_nlabel = create_nlabel(type);
  uint8_t *nlabel = NULL;
  struct rr_entry *e = NULL;
  struct rr_data_txt *tail = NULL;
  int filled = 0;

  if (inst_label != NULL && type_nlabel != NULL)
    nlabel = join_nlabel(inst_label, type_nlabel);
  free(inst_label);
  free(type_nlabel);
  if (nlabel == NULL) {
    warn("tinysvcmdns: cannot form a record name for \"%s\" under %s", instance, type);
    return -1;
  }

  /* The same call mdnsd_register_svc makes, so the record carries the same
   * class, cache-flush bit and 4500-second TTL as every other one on the
   * air. */
  e = rr_create(nlabel, RR_TXT);
  if (e == NULL) {
    free(nlabel);
    warn("tinysvcmdns: out of memory creating the TXT record for \"%s\"", instance);
    return -1;
  }

  /* The first string lives in the entry itself and the rest hang off it —
   * tinysvcmdns's own layout, which its encoder walks. */
  tail = &e->data.TXT;
  for (; txt != NULL && *txt != NULL; txt++) {
    uint8_t *label = em_txt_label(*txt);
    if (label == NULL) {
      /* Loud and survivable: one unrepresentable record is worth less than
       * the service it is attached to, and silence here is how a 67-byte
       * string became a segmentation fault. */
      warn("tinysvcmdns: dropping a %zu-byte TXT record from \"%s\" — the limit is %d",
           strlen(*txt), instance, EM_TXT_MAX);
      continue;
    }
    if (!filled) {
      tail->txt = label;
      filled = 1;
      continue;
    }
    struct rr_data_txt *next = calloc(1, sizeof(*next));
    if (next == NULL) {
      free(label);
      warn("tinysvcmdns: out of memory extending the TXT record for \"%s\"", instance);
      break;
    }
    next->txt = label;
    tail->next = next;
    tail = next;
  }

  /* An empty TXT record is legal and is what a service with no strings is
   * supposed to advertise — but the encoder reads txt[0] unconditionally, so
   * the single embedded node must hold something. A zero-length string is
   * the encoding of "no attributes". */
  if (!filled) {
    tail->txt = em_txt_label("");
    if (tail->txt == NULL) {
      warn("tinysvcmdns: out of memory creating an empty TXT record for \"%s\"", instance);
      return -1;
    }
  }

  mdnsd_add_rr(svr, e);
  return 0;
}

static int register_one(const char *instance, const char *regtype, const char *txt[]) {
  char type[128];
  if (regtype == NULL || em_regtype_local(regtype, type, sizeof(type)) != 0) {
    warn("tinysvcmdns: cannot form a service type from \"%s\"", regtype ? regtype : "(null)");
    return -1;
  }
  /* NULL rather than txt: the TXT record is built by add_txt_record below,
   * for the reason in its comment. Passing the strings here is what crashes
   * the responder. */
  struct mdns_service *svc = mdnsd_register_svc(svr, instance, type, ad.port, NULL, NULL);
  if (svc == NULL) {
    warn("tinysvcmdns: could not register %s as \"%s\"", type, instance);
    return -1;
  }
  /* Frees the wrapper only — the records themselves belong to the responder
   * and are freed by mdnsd_stop. Upstream does the same. */
  mdns_service_destroy(svc);

  if (add_txt_record(instance, type, txt) != 0) {
    warn("tinysvcmdns: %s as \"%s\" has no TXT record", type, instance);
    return -1;
  }
  return 0;
}

/* Start a responder and put the whole current advertisement on it. Called for
 * the first registration and again for every update. Caller holds ad_lock. */
static int bring_up(void) {
  svr = mdnsd_start();
  if (svr == NULL) {
    warn("tinysvcmdns: mdnsd_start() failed");
    return -1;
  }

  if (set_hostname_and_addresses() != 0) {
    mdnsd_stop(svr); /* upstream leaked the responder and its thread here */
    svr = NULL;
    return -1;
  }

  /* The primary service's records come from the caller — see the header
   * comment. The mdns.h macros remain as the fallback for a caller that
   * passed none, so a NULL argument is a service with upstream's records
   * rather than a service with no TXT record at all. They are built here
   * rather than returned from a helper because the macros are not constant
   * expressions (config.password decides the last one), so they cannot
   * initialise anything with static storage. */
  char *txt_without[] = {MDNS_RECORD_WITHOUT_METADATA, NULL};
#ifdef CONFIG_METADATA
  char *txt_with[] = {MDNS_RECORD_WITH_METADATA, NULL};
#endif
  char **fallback = txt_without;
#ifdef CONFIG_METADATA
  if (config.metadata_enabled)
    fallback = txt_with;
#endif
  char **primary = (ad.primary != NULL) ? ad.primary : fallback;

  if (register_one(ad.ap1name, config.regtype, (const char **)primary) != 0) {
    mdnsd_stop(svr);
    svr = NULL;
    return -1;
  }

  /* The second service is the whole point of this file, and it is also the
   * half that must not take the first one down with it: a device that answers
   * classic AirPlay is worth more than one that answers nothing. So a failure
   * here is loud and survivable. */
  if (em_ad_has_second_service(&ad, config.regtype2)) {
    if (register_one(ad.ap2name, config.regtype2, (const char **)ad.secondary) != 0)
      warn("tinysvcmdns: %s is advertised but %s is NOT — this device will be found for "
           "classic AirPlay and not for AirPlay 2",
           config.regtype, config.regtype2);
    else
      debug(1, "tinysvcmdns: advertised %s as \"%s\" and %s as \"%s\".", config.regtype,
            ad.ap1name, config.regtype2, ad.ap2name);
  } else {
    debug(1, "tinysvcmdns: advertised %s as \"%s\"; no AirPlay 2 service.", config.regtype,
          ad.ap1name);
  }

  return 0;
}

/* Caller holds ad_lock. */
static void tear_down(void) {
  if (svr != NULL) {
    mdnsd_stop(svr);
    svr = NULL;
  }
}

static int mdns_tinysvcmdns_register(char *ap1name, char *ap2name, int port, char **txt_records,
                                     char **secondary_txt_records) {
  int rc;

  pthread_mutex_lock(&ad_lock);
  tear_down(); /* mdns_register is called once, but make it idempotent */
  if (em_ad_set(&ad, ap1name, ap2name, port, txt_records, secondary_txt_records) != 0) {
    warn("tinysvcmdns: out of memory building the advertisement");
    pthread_mutex_unlock(&ad_lock);
    return -1;
  }
  rc = bring_up();
  pthread_mutex_unlock(&ad_lock);
  return rc;
}

static int mdns_tinysvcmdns_update(char **txt_records, char **secondary_txt_records) {
  int rc;

  pthread_mutex_lock(&ad_lock);
  if (ad.ap1name == NULL) {
    /* Nothing registered. Upstream's mdns.c only calls update through
     * config.mdns, which is set by a successful register, so this is a
     * can't-happen — and a can't-happen that restarts a responder from an
     * empty advertisement would take the device off the air. */
    pthread_mutex_unlock(&ad_lock);
    debug(1, "tinysvcmdns: update before register, ignored.");
    return -1;
  }

  if (em_ad_update(&ad, txt_records, secondary_txt_records) != 0) {
    warn("tinysvcmdns: out of memory updating the advertisement; keeping the old records");
    pthread_mutex_unlock(&ad_lock);
    return -1;
  }

  /* Restart, because announcing is what makes the change visible — an edited
   * record with a 4500-second TTL is not. */
  tear_down();
  rc = bring_up();
  if (rc != 0)
    warn("tinysvcmdns: the responder did not come back after an update — this device is no "
         "longer advertised");
  pthread_mutex_unlock(&ad_lock);
  return rc;
}

static void mdns_tinysvcmdns_unregister(void) {
  pthread_mutex_lock(&ad_lock);
  tear_down();
  em_ad_free(&ad);
  pthread_mutex_unlock(&ad_lock);
}

mdns_backend mdns_tinysvcmdns = {.name = "tinysvcmdns",
                                 .mdns_register = mdns_tinysvcmdns_register,
                                 .mdns_update = mdns_tinysvcmdns_update,
                                 .mdns_unregister = mdns_tinysvcmdns_unregister,
                                 .mdns_dacp_monitor_start = NULL,
                                 .mdns_dacp_monitor_set_id = NULL,
                                 .mdns_dacp_monitor_stop = NULL};
