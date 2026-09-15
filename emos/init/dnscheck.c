/* Prove the DNS proxy answers bytes bionic's own client can read.
 *
 * This exists because every way of getting it wrong is SILENT on hardware.
 * getaddrinfo returns EAI_NODATA whatever the cause, nothing is logged at
 * either end, and the program that asked reports only that a name did not
 * resolve — which is indistinguishable from the network being down, from a
 * wrong nameserver, and from the socket not being there at all. There is no
 * observation on the device that separates them.
 *
 * The one that would have shipped is the addrinfo LENGTH. Every caller is a
 * 32-bit bionic process, so the struct on the wire is 32 bytes; built for
 * FireOS 5's aarch64 kernel this init's own `struct addrinfo` is 48. Writing
 * sizeof would pass every compile, work on one kernel and refuse every name on
 * the other — and the second case is the one that has never been through a
 * full install, so it would have been found by a user.
 *
 * init.c is included whole, the same trick ringsim.c, pwcheck.c, tmoutcheck.c
 * and wpacheck.c use, so this drives the real functions rather than a copy.
 *
 *   cc -O2 -o dnscheck dnscheck.c && ./dnscheck
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/wait.h>

#define LEDDIR "/tmp/emos-dnscheck"
#define main   init_main_unused

#include "init.c"

#undef main

static int failures;

static void ok(const char *label, int cond, const char *detail)
{
    if (cond) {
        printf("ok    %s\n", label);
    } else {
        printf("FAIL  %s%s%s\n", label, detail ? ": " : "", detail ? detail : "");
        failures++;
    }
}

static unsigned rd32be(const unsigned char *p)
{
    return ((unsigned)p[0] << 24) | ((unsigned)p[1] << 16)
         | ((unsigned)p[2] << 8) | p[3];
}

/* ── The wire structs ────────────────────────────────────────────────────── */

static void check_ai_wire(void)
{
    ok("the addrinfo on the wire is 32 bytes, never this init's sizeof",
       AI_WIRE_LEN == 32, NULL);

    unsigned char b[AI_WIRE_LEN];
    ai_wire(b, 0x11, AF_INET, SOCK_STREAM, IPPROTO_TCP, 16);

    int v;
    memcpy(&v, b + 0,  4); ok("ai_flags at offset 0",    v == 0x11, NULL);
    memcpy(&v, b + 4,  4); ok("ai_family at offset 4",   v == AF_INET, NULL);
    memcpy(&v, b + 8,  4); ok("ai_socktype at offset 8", v == SOCK_STREAM, NULL);
    memcpy(&v, b + 12, 4); ok("ai_protocol at offset 12",v == IPPROTO_TCP, NULL);
    memcpy(&v, b + 16, 4); ok("ai_addrlen at offset 16", v == 16, NULL);

    /* The client nulls the three pointers itself, but a blob carrying rubbish
     * there would still be wrong: it is what a future field would land in. */
    int tail_clear = 1;
    for (int i = 20; i < AI_WIRE_LEN; i++)
        if (b[i]) tail_clear = 0;
    ok("everything past ai_addrlen is zero", tail_clear, NULL);
}

static void check_sin_wire(void)
{
    unsigned char b[16];
    uint32_t addr;
    inet_pton(AF_INET, "192.168.178.140", &addr);
    sin_wire(b, addr, 443);

    unsigned short fam;
    memcpy(&fam, b, 2);
    ok("sin_family is HOST order", fam == AF_INET, NULL);
    ok("sin_port is NETWORK order", b[2] == 0x01 && b[3] == 0xbb, NULL);
    ok("sin_addr is network order, unswapped end to end",
       b[4] == 192 && b[5] == 168 && b[6] == 178 && b[7] == 140, NULL);
}

/* ── Query building ──────────────────────────────────────────────────────── */

static void check_query(void)
{
    unsigned char q[512];
    int n = dns_build_query(q, sizeof q, "apresolve.spotify.com", 1, 0xbeef);

    ok("a query is built", n > 12, NULL);
    ok("the id rides the header", q[0] == 0xbe && q[1] == 0xef, NULL);
    ok("recursion is requested", q[2] == 0x01, NULL);
    ok("one question, no answers", q[5] == 1 && q[7] == 0, NULL);
    ok("labels are length-prefixed",
       q[12] == 9 && !memcmp(q + 13, "apresolve", 9) && q[22] == 7, NULL);
    ok("the name ends with a zero label and A/IN follows",
       q[n - 5] == 0 && q[n - 4] == 0 && q[n - 3] == 1
       && q[n - 2] == 0 && q[n - 1] == 1, NULL);

    char toolong[80];
    memset(toolong, 'a', sizeof toolong - 1);
    toolong[sizeof toolong - 1] = 0;
    ok("a label over 63 bytes is REFUSED, not truncated",
       dns_build_query(q, sizeof q, toolong, 1, 1) < 0,
       "a truncated name asks about a different host and answers confidently");

    ok("a name that will not fit the buffer is refused",
       dns_build_query(q, 20, "apresolve.spotify.com", 1, 1) < 0, NULL);
}

/* ── Answer parsing ──────────────────────────────────────────────────────── */

/* Build an answer by hand: one question, then `n` records. Compression is used
 * for every owner name, because a real server does and a parser that only
 * handles the uncompressed form works against a test and not against a
 * router. */
static int build_answer(unsigned char *m, unsigned id, int rcode,
                        int with_cname, const char *const *ips, int n)
{
    int off = 0;
    m[off++] = (unsigned char)(id >> 8);
    m[off++] = (unsigned char)id;
    m[off++] = 0x81;                        /* QR + RD */
    m[off++] = (unsigned char)(0x80 | rcode);
    m[off++] = 0; m[off++] = 1;             /* QDCOUNT */
    int ancount_at = off;
    m[off++] = 0; m[off++] = (unsigned char)(n + (with_cname ? 1 : 0));
    m[off++] = 0; m[off++] = 0;
    m[off++] = 0; m[off++] = 0;

    m[off++] = 4; memcpy(m + off, "test", 4); off += 4;
    m[off++] = 3; memcpy(m + off, "com", 3);  off += 3;
    m[off++] = 0;
    m[off++] = 0; m[off++] = 1;
    m[off++] = 0; m[off++] = 1;

    if (with_cname) {
        m[off++] = 0xc0; m[off++] = 12;     /* compressed owner */
        m[off++] = 0; m[off++] = 5;         /* CNAME */
        m[off++] = 0; m[off++] = 1;
        m[off++] = 0; m[off++] = 0; m[off++] = 0; m[off++] = 60;
        m[off++] = 0; m[off++] = 8;         /* rdlength */
        m[off++] = 5; memcpy(m + off, "other", 5); off += 5;
        m[off++] = 0xc0; m[off++] = 17;     /* pointer to "com" */
    }

    for (int i = 0; i < n; i++) {
        m[off++] = 0xc0; m[off++] = 12;
        m[off++] = 0; m[off++] = 1;         /* A */
        m[off++] = 0; m[off++] = 1;
        m[off++] = 0; m[off++] = 0; m[off++] = 0; m[off++] = 60;
        m[off++] = 0; m[off++] = 4;
        uint32_t a;
        inet_pton(AF_INET, ips[i], &a);
        memcpy(m + off, &a, 4); off += 4;
    }
    (void)ancount_at;
    return off;
}

static void check_parse(void)
{
    unsigned char m[512];
    uint32_t out[8];
    const char *ips[] = { "104.18.1.1", "104.18.2.2" };

    int len = build_answer(m, 0x1234, 0, 0, ips, 2);
    int n = dns_parse_a(m, len, 0x1234, out, 8);
    ok("both A records come back", n == 2, NULL);
    char buf[32];
    inet_ntop(AF_INET, &out[0], buf, sizeof buf);
    ok("the first address survives the round trip", !strcmp(buf, "104.18.1.1"), buf);

    len = build_answer(m, 0x1234, 0, 1, ips, 2);
    n = dns_parse_a(m, len, 0x1234, out, 8);
    ok("a CNAME in front of the A records is walked past", n == 2, NULL);

    len = build_answer(m, 0x1234, 0, 0, ips, 2);
    ok("an answer to somebody else's query is refused",
       dns_parse_a(m, len, 0x9999, out, 8) < 0,
       "a late reply to a previous lookup would otherwise be taken");

    len = build_answer(m, 0x1234, 3 /* NXDOMAIN */, 0, ips, 0);
    ok("NXDOMAIN is refused rather than read as zero addresses",
       dns_parse_a(m, len, 0x1234, out, 8) < 0, NULL);

    len = build_answer(m, 0x1234, 0, 0, ips, 2);
    ok("a truncated message is refused, not parsed off the end",
       dns_parse_a(m, len - 3, 0x1234, out, 8) < 0, NULL);

    ok("a message shorter than a header is refused",
       dns_parse_a(m, 4, 0x1234, out, 8) < 0, NULL);

    unsigned char q[4] = { 0, 0, 0, 0 };
    ok("skipping a name stops at a pointer rather than chasing a loop",
       dns_skip_name((unsigned char[]){ 0xc0, 0x00 }, 2, 0) == 2, NULL);
    (void)q;
}

/* ── The request line ────────────────────────────────────────────────────── */

static void check_gai_parse(void)
{
    struct gai_req r;

    ok("a full request parses",
       gai_parse("getaddrinfo apresolve.spotify.com 443 0 0 1 6 0", &r) == 0
       && !strcmp(r.host, "apresolve.spotify.com")
       && !strcmp(r.serv, "443") && r.socktype == 1, NULL);

    ok("^ means NULL for both host and service",
       gai_parse("getaddrinfo ^ ^ -1 -1 -1 -1 0", &r) == 0
       && r.host[0] == 0 && r.serv[0] == 0, NULL);

    ok("the no-hints form with -1 everywhere parses",
       gai_parse("getaddrinfo spotify.com ^ -1 -1 -1 -1 0", &r) == 0
       && r.family == -1 && r.socktype == -1, NULL);

    ok("a short line is refused", gai_parse("getaddrinfo spotify.com", &r) < 0, NULL);
    ok("another command is refused", gai_parse("gethostbyname spotify.com 2 0", &r) < 0, NULL);

    ok("a numeric service becomes its port", serv_port("443") == 443, NULL);
    ok("no service is port 0", serv_port("") == 0, NULL);
    ok("an unknown service name does not fail the lookup", serv_port("gopher") == 0, NULL);
}

/* ── End to end, over a real socket ──────────────────────────────────────── */

/* Drive dnsproxy_serve exactly as bionic does and read the reply back byte for
 * byte. A literal address is used so nothing here needs a nameserver — the
 * point is the FRAMING, which is what the client is strict about. */
static void check_serve_literal(void)
{
    int sv[2];
    if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv) < 0) {
        ok("socketpair", 0, strerror(errno));
        return;
    }

    pid_t p = fork();
    if (p == 0) {
        close(sv[0]);
        dnsproxy_serve(sv[1]);
        close(sv[1]);
        _exit(0);
    }
    close(sv[1]);

    const char *req = "getaddrinfo 1.2.3.4 443 0 2 1 6 0";
    write(sv[0], req, strlen(req) + 1);      /* the NUL is part of it */

    unsigned char r[512];
    int n = 0, got;
    while ((got = (int)read(sv[0], r + n, sizeof r - (size_t)n)) > 0)
        n += got;
    close(sv[0]);
    waitpid(p, NULL, 0);

    ok("the reply opens with the 4-byte code 222", n >= 4 && !memcmp(r, "222 ", 4),
       n >= 4 ? (char[]){ r[0], r[1], r[2], r[3], 0 } : "nothing");
    if (n < 4)
        return;

    int off = 4;
    ok("the addrinfo is announced as 32 bytes",
       rd32be(r + off) == AI_WIRE_LEN, NULL);
    off += 4;

    int fam;
    memcpy(&fam, r + off + 4, 4);
    ok("the entry is AF_INET", fam == AF_INET, NULL);
    off += AI_WIRE_LEN;

    ok("the sockaddr is announced as 16 bytes", rd32be(r + off) == 16, NULL);
    off += 4;
    ok("the port asked for comes back", r[off + 2] == 0x01 && r[off + 3] == 0xbb, NULL);
    ok("a literal address is NOT sent to a nameserver",
       r[off + 4] == 1 && r[off + 5] == 2 && r[off + 6] == 3 && r[off + 7] == 4, NULL);
    off += 16;

    ok("the canonical name is absent", rd32be(r + off) == 0, NULL);
    off += 4;

    ok("the list is terminated by a zero length", rd32be(r + off) == 0, NULL);
    off += 4;
    ok("and nothing follows it", off == n, NULL);
}

/* A failure has to be a code the client rejects FOLLOWED BY four more bytes:
 * it reads four, and on anything but 222 it consumes four more before giving
 * up. Sending only the code leaves the caller blocked in fread. */
static void check_serve_failure(void)
{
    int sv[2];
    if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv) < 0)
        return;

    pid_t p = fork();
    if (p == 0) {
        close(sv[0]);
        dnsproxy_serve(sv[1]);
        close(sv[1]);
        _exit(0);
    }
    close(sv[1]);

    const char *req = "getaddrinfo ^ ^ -1 -1 -1 -1 0";   /* no host to resolve */
    write(sv[0], req, strlen(req) + 1);

    unsigned char r[64];
    int n = 0, got;
    while ((got = (int)read(sv[0], r + n, sizeof r - (size_t)n)) > 0)
        n += got;
    close(sv[0]);
    waitpid(p, NULL, 0);

    ok("a failure answers a code that is not 222", n >= 4 && memcmp(r, "222 ", 4), NULL);
    ok("and eight bytes in total, or the caller blocks in fread", n == 8, NULL);
}

int main(void)
{
    check_ai_wire();
    check_sin_wire();
    check_query();
    check_parse();
    check_gai_parse();
    check_serve_literal();
    check_serve_failure();

    printf("\n%s\n", failures ? "FAILURES" : "all good");
    return failures ? 1 : 0;
}
