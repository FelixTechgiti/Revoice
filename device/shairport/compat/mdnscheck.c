/* Prove the AirPlay 2 advertisement logic on a host, before any of it is
 * cross-compiled or put in front of an iPhone.
 *
 * mdns_ap2.c is included whole — the emos/init/pwcheck.c trick, the same one
 * shmcheck.c uses — so this drives the real functions rather than a copy.
 *
 *   cc -O2 -o mdnscheck mdnscheck.c && ./mdnscheck
 *
 * What it cannot cover: the backend glue in mdns_tinysvcmdns.c, which is
 * sockets and a responder thread. That is why the decisions live here and the
 * glue is kept thin — the one mistake this catches cheaply is an update that
 * quietly retires records it was not asked to change, which on a device
 * presents as an AirPlay 2 speaker that vanishes from the picker some minutes
 * after the first song.
 *
 * Exits non-zero on the first failure and says which. Run by CI beside
 * shmcheck, ringsim --check and pwcheck.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mdns_ap2.c"

static int failures = 0;

static void ok(int cond, const char *what) {
  printf("%-64s %s\n", what, cond ? "ok" : "FAIL");
  if (!cond)
    failures++;
}

static void eq(const char *got, const char *want, const char *what) {
  int good = got != NULL && strcmp(got, want) == 0;
  printf("%-64s %s\n", what, good ? "ok" : "FAIL");
  if (!good) {
    printf("    got:  %s\n", got ? got : "(null)");
    printf("    want: %s\n", want);
    failures++;
  }
}

static int txt_len(char **t) {
  int n = 0;
  if (t == NULL)
    return 0;
  while (t[n])
    n++;
  return n;
}

/* The AirPlay 2 secondary set, shortened. The first record is constant and the
 * ones after it are what an update actually moves — flags on SETUP/TEARDOWN,
 * gid and gcgl on joining or leaving a group. */
static char *SEC_A[] = {"srcvers=366.0", "deviceid=02:EC:09:F7:11:F9", "flags=0x4",
                        "gid=6E1C...", "gcgl=0", NULL};
static char *SEC_B[] = {"srcvers=366.0", "deviceid=02:EC:09:F7:11:F9", "flags=0x804",
                        "gid=9A2F...", "gcgl=1", "isGroupLeader=0", NULL};

/* The _raop._tcp set an AirPlay 2 build passes, shortened to the four records
 * that tell it apart from the classic one (#310). `pk=` is the long one — 67
 * bytes on every device — and is the record upstream's TXT path could not
 * encode at all, which is why it is in the fixture rather than elided. */
static char *PRI_AP2[] = {"cn=0,1", "et=0,1", "ft=0x405F4A00,0x1C340", "tp=UDP", "vs=366.0",
                          "pk=f2d11075802298c0907285d833b6dd08b04240987f1c95233ab9251e732925f7",
                          NULL};
static char *PRI_CLASSIC[] = {"sf=0x4", "am=ShairportSync", "vs=105.1", "tp=TCP,UDP",
                              "txtvers=1", NULL};

int main(void) {
  char buf[256];

  /* ---- names ------------------------------------------------------- */
  ok(em_regtype_local("_raop._tcp", buf, sizeof buf) == 0, "regtype: accepts _raop._tcp");
  eq(buf, "_raop._tcp.local", "regtype: .local appended");
  ok(em_regtype_local("_airplay._tcp", buf, sizeof buf) == 0, "regtype: accepts _airplay._tcp");
  eq(buf, "_airplay._tcp.local", "regtype: .local appended to the second service");
  ok(em_regtype_local("_raop._tcp.local", buf, sizeof buf) == 0, "regtype: accepts a full name");
  eq(buf, "_raop._tcp.local", "regtype: .local is not doubled");

  eq((em_hostname_local("kitchen", buf, sizeof buf) == 0) ? buf : NULL, "kitchen.local",
     "hostname: .local appended");
  eq((em_hostname_local("kitchen.local", buf, sizeof buf) == 0) ? buf : NULL, "kitchen.local",
     "hostname: .local is not doubled");
  /* A name that does not fit must fail rather than truncate: tinysvcmdns will
   * happily answer for a half a hostname, and the device is then advertising
   * an address nobody can resolve. */
  ok(em_hostname_local("kitchen", buf, 8) == -1, "hostname: a short buffer fails, not truncates");
  ok(em_hostname_local("", buf, sizeof buf) == -1, "hostname: an empty name is rejected");
  ok(em_hostname_local(NULL, buf, sizeof buf) == -1, "hostname: NULL is rejected");

  /* ---- the advertisement ------------------------------------------- */
  struct em_ad ad;
  memset(&ad, 0, sizeof ad);

  ok(em_ad_set(&ad, "02EC09F711F9@Kitchen", "Kitchen", 7000, PRI_AP2, SEC_A) == 0, "set: AirPlay 2 shape");
  eq(ad.ap1name, "02EC09F711F9@Kitchen", "set: the _raop instance name is kept");
  eq(ad.ap2name, "Kitchen", "set: the _airplay instance name is kept");
  ok(ad.port == 7000, "set: the port is kept");
  ok(txt_len(ad.secondary) == 5, "set: all five secondary records are kept");
  ok(ad.secondary[0] != SEC_A[0], "set: the records are COPIED, not aliased");
  eq(ad.secondary[0], "srcvers=366.0", "set: and copied faithfully");

  /* #310: the primary set is held too. It was discarded, so _raop._tcp went
   * on the air carrying the classic macros while _airplay._tcp carried the
   * AirPlay 2 records — a device every client offers and none can play to. */
  ok(txt_len(ad.primary) == 6, "set: the _raop records are kept, not discarded");
  ok(ad.primary[0] != PRI_AP2[0], "set: the _raop records are COPIED too");
  eq(ad.primary[2], "ft=0x405F4A00,0x1C340", "set: ft= survives, which the macros have no field for");
  eq(ad.primary[5], PRI_AP2[5], "set: the 67-byte pk= record survives intact");

  ok(em_ad_has_second_service(&ad, "_airplay._tcp") == 1, "second service: advertised");

  /* The whole reason this struct exists. shairport's update call sites pass
   * NULL for the primary records; a backend that read that as "clear" would
   * retire _raop._tcp's TXT the first time somebody joined a group. */
  ok(em_ad_update(&ad, NULL, NULL) == 0, "update: a NULL set is accepted");
  ok(txt_len(ad.secondary) == 5, "update: NULL KEEPS the current records");
  eq(ad.secondary[2], "flags=0x4", "update: and keeps them unchanged");
  ok(txt_len(ad.primary) == 6, "update: NULL keeps the _raop records as well");

  ok(em_ad_update(&ad, NULL, SEC_B) == 0, "update: a new set is accepted");
  ok(txt_len(ad.secondary) == 6, "update: the new set replaces the old one entirely");
  eq(ad.secondary[2], "flags=0x804", "update: flags moved");
  eq(ad.secondary[5], "isGroupLeader=0", "update: a record the old set did not have");
  eq(ad.ap1name, "02EC09F711F9@Kitchen", "update: leaves the names alone");
  ok(ad.port == 7000, "update: leaves the port alone");
  eq(ad.primary[5], PRI_AP2[5], "update: a secondary-only update leaves _raop's records alone");

  /* Both halves move independently, because the backend passes both through
   * and shairport is free to resend either. */
  ok(em_ad_update(&ad, PRI_CLASSIC, NULL) == 0, "update: the primary half alone is accepted");
  ok(txt_len(ad.primary) == 5, "update: the new _raop set replaces the old one");
  ok(txt_len(ad.secondary) == 6, "update: and leaves the _airplay set alone");

  em_ad_free(&ad);
  ok(ad.ap1name == NULL && ad.primary == NULL && ad.secondary == NULL, "free: clears the struct");

  /* ---- the classic shape ------------------------------------------- */
  /* A classic build passes no ap2name and no secondary records, and
   * config.regtype2 is never set. The same file has to be right for it, which
   * is what lets this replace upstream's backend rather than fork the build. */
  memset(&ad, 0, sizeof ad);
  ok(em_ad_set(&ad, "02EC09F711F9@Kitchen", NULL, 7000, PRI_CLASSIC, NULL) == 0, "set: classic shape");
  ok(ad.ap2name == NULL && ad.secondary == NULL, "classic: no second service is held");
  /* The classic build passes records too, and they are the macros' own set.
   * Holding them changes nothing about what goes on the air and is what lets
   * one code path serve both flavours. */
  ok(txt_len(ad.primary) == 5, "classic: the _raop records are held all the same");
  eq(ad.primary[2], "vs=105.1", "classic: and are the classic ones, unchanged");
  ok(em_ad_has_second_service(&ad, NULL) == 0, "classic: no regtype2 -> not advertised");
  ok(em_ad_has_second_service(&ad, "_airplay._tcp") == 0,
     "classic: regtype2 alone is not enough without records");

  /* An AirPlay 2 build whose records have not been built yet must also not
   * advertise: an _airplay._tcp instance with an empty TXT is found by a
   * client, asked what it supports, and answers nothing. */
  char *empty[] = {NULL};
  ok(em_ad_set(&ad, "02EC09F711F9@Kitchen", "Kitchen", 7000, PRI_AP2, empty) == 0, "set: empty record set");
  ok(em_ad_has_second_service(&ad, "_airplay._tcp") == 0,
     "empty records -> the second service stays off the air");
  em_ad_free(&ad);

  /* ---- degenerate input -------------------------------------------- */
  memset(&ad, 0, sizeof ad);
  ok(em_ad_set(&ad, NULL, NULL, 7000, NULL, NULL) == -1, "set: a NULL _raop name is rejected");
  ok(em_ad_update(NULL, NULL, SEC_A) == -1, "update: a NULL advertisement is rejected");
  ok(em_ad_has_second_service(NULL, "_airplay._tcp") == 0, "second service: NULL is not advertised");
  em_ad_free(&ad);
  em_ad_free(NULL); /* must not crash */

  printf("\n%s\n", failures ? "FAILURES" : "all checks passed");
  return failures ? 1 : 0;
}
