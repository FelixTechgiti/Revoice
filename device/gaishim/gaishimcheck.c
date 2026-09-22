/*
 * gaishimcheck — drives the real getaddrinfo shim on the build host
 * =================================================================
 *
 * Built and run by CI, and by hand in one line (see build.sh). It `#include`s
 * `gaishim.c` WHOLE and calls the exported `getaddrinfo`, for the reason
 * `emos/init`'s checkers do it that way: a checker that reimplements the thing
 * it checks drifts from the device, and every failure this file covers is
 * silent on hardware — a receiver that binds nothing, a client that never
 * sends a SYN, a sockaddr with a hostname pointer in it.
 *
 * Three hooks are replaced and nothing else is:
 *
 *   - `gethostbyname`, because the real one would do real DNS from a CI runner
 *     and because the cases worth checking are the ones a resolver cannot be
 *     asked to produce on demand (no addresses, the wrong family, eight
 *     addresses at once).
 *   - `getservbyname`, so "https" means 443 here whatever /etc/services says.
 *   - `inet_pton`, ONLY to translate the address family: AF_INET6 is 10 on
 *     Linux and 30 on macOS, and the shim necessarily carries Android's
 *     number. The parse itself is the host libc's, which is the point.
 *
 * `malloc` and `free` are counted, so the error paths — which free a partly
 * built list — are checked for leaks and double frees rather than only for
 * their return value.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <fcntl.h>
#include <unistd.h>

static long live_blocks;
static void *chk_malloc(unsigned long n) { live_blocks++; return malloc(n); }
static void  chk_free(void *p)           { if (p) live_blocks--; free(p); }

struct hostent;
struct servent;
static struct hostent *stub_gethostbyname(const char *);
static struct servent *stub_getservbyname(const char *, const char *);
static int              stub_pton(int, const char *, void *);

/* The constructor is the one thing this file must NOT get: it would run at
 * process start on the host and resolve a name through the stubs before any
 * test has set them up. `gaishim_selftest` itself is still driven below, by
 * hand, which is the part worth checking. */
/* No resolv.conf here. The build host has a real one, and without this the
 * checker does REAL DNS for the made-up names below — which answers NXDOMAIN,
 * which dns_lookup correctly reports as a definitive "no such name", so the
 * stubbed resolver is never reached and eleven tests fail for the right
 * reason about the wrong thing. With no nameservers the lookup answers
 * "could not ask" and falls through to the stub, which is what the tests are
 * about. The wire format itself is checked directly, further down. */
#define GAISHIM_RESOLV "/nonexistent/gaishim-test-resolv.conf"

#define GAISHIM_TEST

#define malloc           chk_malloc
#define free             chk_free
#define GAISHIM_RESOLVER stub_gethostbyname
#define GAISHIM_SERVICES stub_getservbyname
#define GAISHIM_PTON     stub_pton

#include "gaishim.c"

/* ── the hooks ──────────────────────────────────────────────────────────── */

static int   stub_calls;          /* how often the resolver was reached */
static int   stub_naddr;          /* how many addresses it answers with */
static int   stub_addrtype = SHIM_AF_INET;
static int   stub_hlen     = 4;
static char  stub_name[64] = "canon.example.test";

static unsigned char stub_bytes[SHIM_MAX_ADDRS][4];
static char         *stub_list[SHIM_MAX_ADDRS + 1];
static struct hostent stub_he;

static struct hostent *stub_gethostbyname(const char *node)
{
	int i;
	stub_calls++;
	(void)node;
	if (stub_naddr <= 0)
		return 0;
	for (i = 0; i < stub_naddr; i++) {
		stub_bytes[i][0] = 10;
		stub_bytes[i][1] = 0;
		stub_bytes[i][2] = 0;
		stub_bytes[i][3] = (unsigned char)(i + 1);
		stub_list[i] = (char *)stub_bytes[i];
	}
	stub_list[stub_naddr] = 0;
	stub_he.h_name      = stub_name;
	stub_he.h_aliases   = 0;
	stub_he.h_addrtype  = stub_addrtype;
	stub_he.h_length    = stub_hlen;
	stub_he.h_addr_list = stub_list;
	return &stub_he;
}

static struct servent stub_se;
static char           stub_proto[8];

static struct servent *stub_getservbyname(const char *name, const char *proto)
{
	unsigned short be;
	unsigned char b[2];
	unsigned port;

	if (!strcmp(name, "https"))     port = 443;
	else if (!strcmp(name, "http")) port = 80;
	else                            return 0;

	b[0] = (unsigned char)(port >> 8);
	b[1] = (unsigned char)(port & 0xff);
	memcpy(&be, b, 2);

	strncpy(stub_proto, proto, sizeof stub_proto - 1);
	stub_se.s_name  = (char *)name;
	stub_se.s_port  = (int)be;
	stub_se.s_proto = stub_proto;
	return &stub_se;
}

static int stub_pton(int shim_af, const char *src, void *dst)
{
	int host_af = (shim_af == SHIM_AF_INET6) ? AF_INET6 : AF_INET;
	return inet_pton(host_af, src, dst);
}

/* ── the harness ────────────────────────────────────────────────────────── */

static int failures;

static void ok(int cond, const char *what)
{
	if (!cond) {
		printf("FAIL  %s\n", what);
		failures++;
	}
}

static void reset(void)
{
	stub_calls   = 0;
	stub_naddr   = 1;
	stub_addrtype = SHIM_AF_INET;
	stub_hlen    = 4;
}

static int entries(struct addrinfo *ai)
{
	int n = 0;
	for (; ai; ai = ai->ai_next)
		n++;
	return n;
}

/* The sockaddr as the kernel would read it: family, then the port's two bytes
 * in network order, then the address bytes. Checked bytewise on purpose — a
 * host-order port is the classic silent one, because it connects to SOMETHING. */
static int v4_is(struct addrinfo *ai, const char *dotted, unsigned port)
{
	unsigned char want[4], got[4], pb[2];
	struct shim_sockaddr_in *s;

	if (!ai || ai->ai_family != SHIM_AF_INET ||
	    ai->ai_addrlen != sizeof(struct shim_sockaddr_in))
		return 0;
	s = (struct shim_sockaddr_in *)ai->ai_addr;
	if (s->sin_family != SHIM_AF_INET)
		return 0;
	memcpy(pb, &s->sin_port, 2);
	if (pb[0] != (unsigned char)(port >> 8) || pb[1] != (unsigned char)(port & 0xff))
		return 0;
	if (inet_pton(AF_INET, dotted, want) != 1)
		return 0;
	memcpy(got, &s->sin_addr, 4);
	return memcmp(want, got, 4) == 0;
}

static int v6_is(struct addrinfo *ai, const char *text, unsigned port)
{
	unsigned char want[16], pb[2];
	struct shim_sockaddr_in6 *s;

	if (!ai || ai->ai_family != SHIM_AF_INET6 ||
	    ai->ai_addrlen != sizeof(struct shim_sockaddr_in6))
		return 0;
	s = (struct shim_sockaddr_in6 *)ai->ai_addr;
	if (s->sin6_family != SHIM_AF_INET6)
		return 0;
	memcpy(pb, &s->sin6_port, 2);
	if (pb[0] != (unsigned char)(port >> 8) || pb[1] != (unsigned char)(port & 0xff))
		return 0;
	if (inet_pton(AF_INET6, text, want) != 1)
		return 0;
	return memcmp(want, s->sin6_addr, 16) == 0;
}

static struct addrinfo mk(int family, int socktype, int flags)
{
	struct addrinfo h;
	memset(&h, 0, sizeof h);
	h.ai_family   = family;
	h.ai_socktype = socktype;
	h.ai_flags    = flags;
	return h;
}

int main(void)
{
	struct addrinfo *res;
	struct addrinfo  h;
	int rc;

	/* ── the layout the caller was compiled against ──────────────────── */
	{
		struct addrinfo a;
		/* BSD order, not glibc's. Swapping these two compiles and links
		 * and hands every caller a char* where it wants a sockaddr. */
		ok((char *)&a.ai_canonname < (char *)&a.ai_addr,
		   "ai_canonname precedes ai_addr (BSD/Android order)");
		ok((char *)&a.ai_addrlen < (char *)&a.ai_canonname,
		   "ai_addrlen precedes both pointers");
		ok((char *)&a.ai_addr < (char *)&a.ai_next,
		   "ai_next is last");
		ok(sizeof(struct shim_sockaddr_in) == 16, "sockaddr_in is 16 bytes");
		ok(sizeof(struct shim_sockaddr_in6) == 28, "sockaddr_in6 is 28 bytes");
		if (sizeof(void *) == 4)
			ok(sizeof(struct addrinfo) == 32,
			   "addrinfo is 32 bytes in a 32-bit build");
	}

	/* ── a literal needs no resolver, and the port is network order ───── */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("35.186.224.24", "443", &h, &res);
	ok(rc == 0, "literal v4 resolves");
	ok(stub_calls == 0, "literal v4 does not reach the resolver");
	ok(entries(res) == 1, "literal v4 with SOCK_STREAM gives one entry");
	ok(v4_is(res, "35.186.224.24", 443), "literal v4 sockaddr is right");
	ok(res && res->ai_protocol == SHIM_IPPROTO_TCP, "SOCK_STREAM implies TCP");
	freeaddrinfo(res);

	/* ── the passive wildcard: a receiver must bind with no resolver ──── */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, SHIM_AI_PASSIVE);
	rc = getaddrinfo(0, "7000", &h, &res);
	ok(rc == 0 && v4_is(res, "0.0.0.0", 7000), "AI_PASSIVE binds 0.0.0.0");
	ok(stub_calls == 0, "AI_PASSIVE does not reach the resolver");
	freeaddrinfo(res);

	reset();
	h = mk(SHIM_AF_INET6, SHIM_SOCK_STREAM, SHIM_AI_PASSIVE);
	rc = getaddrinfo(0, "7000", &h, &res);
	ok(rc == 0 && v6_is(res, "::", 7000), "AI_PASSIVE binds :: for AF_INET6");
	freeaddrinfo(res);

	/* Without AI_PASSIVE a null node is loopback, not the wildcard. */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo(0, "9000", &h, &res);
	ok(rc == 0 && v4_is(res, "127.0.0.1", 9000), "null node without AI_PASSIVE is loopback");
	freeaddrinfo(res);

	/* ── localhost, which is #219 and which nqptp's control port needs ── */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_DGRAM, 0);
	rc = getaddrinfo("localhost", "9000", &h, &res);
	ok(rc == 0 && v4_is(res, "127.0.0.1", 9000), "localhost is 127.0.0.1");
	ok(stub_calls == 0, "localhost never reaches the resolver");
	ok(res && res->ai_protocol == SHIM_IPPROTO_UDP, "SOCK_DGRAM implies UDP");
	freeaddrinfo(res);

	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("LocalHost", "80", &h, &res);
	ok(rc == 0 && v4_is(res, "127.0.0.1", 80), "localhost is matched case-insensitively");
	freeaddrinfo(res);

	reset();
	h = mk(SHIM_AF_INET6, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("localhost", "80", &h, &res);
	ok(rc == 0 && v6_is(res, "::1", 80), "localhost under AF_INET6 is ::1");
	freeaddrinfo(res);

	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("ip6-localhost", "80", &h, &res);
	ok(rc == 0 && v6_is(res, "::1", 80), "ip6-localhost is v6 even under AF_UNSPEC");
	freeaddrinfo(res);

	/* ── a name, which is the whole point ────────────────────────────── */
	reset();
	stub_naddr = 3;
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("clienttoken.spotify.com", "443", &h, &res);
	ok(rc == 0, "a name resolves through gethostbyname");
	ok(stub_calls == 1, "the resolver is called exactly once");
	ok(entries(res) == 3, "every address the resolver gave is kept");
	ok(v4_is(res, "10.0.0.1", 443), "the first address is first");
	ok(res && res->ai_canonname == 0, "no canonical name unless asked");
	freeaddrinfo(res);

	/* Unqualified socktype gives stream first, then dgram, per address. */
	reset();
	stub_naddr = 2;
	rc = getaddrinfo("host.example.test", "443", 0, &res);
	ok(rc == 0 && entries(res) == 4, "no hints gives stream and dgram per address");
	ok(res && res->ai_socktype == SHIM_SOCK_STREAM, "stream comes first");
	freeaddrinfo(res);

	/* ── AI_CANONNAME sits on the head and nowhere else ──────────────── */
	reset();
	stub_naddr = 2;
	h = mk(SHIM_AF_INET, SHIM_SOCK_STREAM, SHIM_AI_CANONNAME);
	rc = getaddrinfo("host.example.test", "443", &h, &res);
	ok(rc == 0 && res && res->ai_canonname &&
	   !strcmp(res->ai_canonname, "canon.example.test"), "AI_CANONNAME is answered");
	ok(res && res->ai_next && res->ai_next->ai_canonname == 0,
	   "only the head carries the canonical name");
	freeaddrinfo(res);

	/* ── the refusals, each of which must NOT be a wrong answer ──────── */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, SHIM_AI_NUMERICHOST);
	rc = getaddrinfo("host.example.test", "443", &h, &res);
	ok(rc == SHIM_EAI_NONAME, "AI_NUMERICHOST refuses a name");
	ok(stub_calls == 0, "AI_NUMERICHOST does not reach the resolver");

	reset();
	h = mk(SHIM_AF_INET6, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("host.example.test", "443", &h, &res);
	ok(rc == SHIM_EAI_ADDRFAMILY, "an AF_INET6 name lookup is refused, not faked");
	ok(stub_calls == 0, "an AF_INET6 name lookup does not reach the resolver");

	reset();
	stub_naddr = 0;
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("nothing.example.test", "443", &h, &res);
	ok(rc == SHIM_EAI_NODATA, "a name with no addresses is EAI_NODATA");

	reset();
	stub_addrtype = SHIM_AF_INET6;
	stub_hlen     = 16;
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("host.example.test", "443", &h, &res);
	ok(rc == SHIM_EAI_ADDRFAMILY, "a hostent of the wrong family is refused");

	reset();
	h = mk(99, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("host.example.test", "443", &h, &res);
	ok(rc == SHIM_EAI_FAMILY, "an unknown family is EAI_FAMILY");

	reset();
	rc = getaddrinfo(0, 0, 0, &res);
	ok(rc == SHIM_EAI_NONAME, "no node and no service is EAI_NONAME");

	/* ── services ────────────────────────────────────────────────────── */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("localhost", "https", &h, &res);
	ok(rc == 0 && v4_is(res, "127.0.0.1", 443), "a service name becomes its port");
	freeaddrinfo(res);

	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, SHIM_AI_NUMERICSERV);
	rc = getaddrinfo("localhost", "https", &h, &res);
	ok(rc == SHIM_EAI_SERVICE, "AI_NUMERICSERV refuses a service name");

	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("localhost", "nosuchservice", &h, &res);
	ok(rc == SHIM_EAI_SERVICE, "an unknown service is EAI_SERVICE");

	/* A port that is not a clean decimal must not be half-read: "80x" is a
	 * service name, and there is no service called 80x. */
	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("localhost", "80x", &h, &res);
	ok(rc == SHIM_EAI_SERVICE, "a partly numeric service is not port 80");

	reset();
	h = mk(SHIM_AF_UNSPEC, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("localhost", "65536", &h, &res);
	ok(rc == SHIM_EAI_SERVICE, "a port above 65535 is not truncated");

	/* ── the resolver's buffer is copied, never held ─────────────────── */
	reset();
	stub_naddr = 2;
	h = mk(SHIM_AF_INET, SHIM_SOCK_STREAM, 0);
	rc = getaddrinfo("first.example.test", "443", &h, &res);
	ok(rc == 0, "first lookup");
	{
		struct addrinfo *res2;
		int i;
		/* Overwrite the stub's static buffer the way a second call on
		 * the same thread overwrites bionic's. */
		for (i = 0; i < SHIM_MAX_ADDRS; i++)
			stub_bytes[i][0] = 99;
		stub_naddr = 1;
		rc = getaddrinfo("second.example.test", "443", &h, &res2);
		ok(rc == 0, "second lookup");
		ok(v4_is(res, "10.0.0.1", 443),
		   "the first result survives a second lookup");
		freeaddrinfo(res2);
	}
	freeaddrinfo(res);

	/* ── the wire format, where a mistake answers the WRONG question ─── */
	//
	// A truncated or mis-encoded query asks about a different name and gets a
	// confident answer; a sloppy parser returns an address that belongs to
	// something else. Neither shows up as a failure on the device — the
	// endpoint simply connects somewhere unexpected or not at all.
	{
		unsigned char q[512];
		int n = dns_build_query(q, (int)sizeof q, "a.example.com", 0x1234);
		static const unsigned char want[] = {
			0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0,
			1, 'a', 7, 'e','x','a','m','p','l','e', 3, 'c','o','m', 0,
			0, 1, 0, 1,
		};
		ok(n == (int)sizeof want, "query length");
		ok(n == (int)sizeof want && memcmp(q, want, sizeof want) == 0,
		   "query is encoded label by label, with RD set and QTYPE A");

		char longlabel[80];
		memset(longlabel, 'x', sizeof longlabel);
		longlabel[64] = 0;
		ok(dns_build_query(q, (int)sizeof q, longlabel, 1) < 0,
		   "a label over 63 bytes is refused, not truncated");
		ok(dns_build_query(q, 8, "a.example.com", 1) < 0,
		   "a name that does not fit is refused");
	}

	{
		/* One answer, name given as a compression pointer — which is what a
		 * real server sends and what a hand-rolled parser gets wrong. */
		static const unsigned char reply[] = {
			0x12, 0x34, 0x81, 0x80, 0x00, 0x01, 0x00, 0x01, 0, 0, 0, 0,
			1, 'a', 7, 'e','x','a','m','p','l','e', 3, 'c','o','m', 0,
			0, 1, 0, 1,
			0xc0, 0x0c,                      /* pointer to the question */
			0x00, 0x01, 0x00, 0x01,          /* A, IN */
			0x00, 0x00, 0x00, 0x3c,          /* TTL */
			0x00, 0x04, 35, 186, 224, 24,
		};
		unsigned char out[SHIM_MAX_ADDRS][4];
		int n = dns_parse_a(reply, (int)sizeof reply, 0x1234, out, SHIM_MAX_ADDRS);
		ok(n == 1, "one A record is read");
		ok(n == 1 && out[0][0] == 35 && out[0][1] == 186 &&
		   out[0][2] == 224 && out[0][3] == 24,
		   "the address is the one in the packet");

		ok(dns_parse_a(reply, (int)sizeof reply, 0x9999, out, SHIM_MAX_ADDRS) < 0,
		   "a reply whose id does not match is refused");
		ok(dns_parse_a(reply, 8, 0x1234, out, SHIM_MAX_ADDRS) < 0,
		   "a truncated reply is refused");

		unsigned char nx[sizeof reply];
		memcpy(nx, reply, sizeof reply);
		nx[3] = 0x83;                        /* RCODE 3 — NXDOMAIN */
		ok(dns_parse_a(nx, (int)sizeof reply, 0x1234, out, SHIM_MAX_ADDRS) == 0,
		   "NXDOMAIN is an answer (0), not a failure (-1) — it must not be retried");
	}

	{
		/* The file emOS writes, and the shapes it can take. */
		const char *path = "/tmp/gaishim-test-resolv.conf";
		FILE *f = fopen(path, "w");
		unsigned char srv[3][4];
		int n;
		ok(f != NULL, "temp resolv.conf opened");
		if (f) {
			fputs("# comment\n  nameserver 192.168.178.1\n"
			      "nameserver 1.1.1.1\nsearch lan\n", f);
			fclose(f);
		}
		n = dns_servers_from(path, srv, 3);
		ok(n == 2, "both nameservers are found, comments and search ignored");
		ok(n >= 1 && srv[0][0] == 192 && srv[0][3] == 1,
		   "the first nameserver is parsed, leading whitespace and all");
		ok(dns_servers_from("/nonexistent/nope", srv, 3) == 0,
		   "a missing resolv.conf is zero servers, not a crash");
		remove(path);
	}

	/* ── the self-test, which is what the device will report ─────────── */
	//
	// It writes to stderr rather than returning a value, because it runs
	// inside somebody else's process and has nowhere else to put an answer.
	// Checked here for the two things the firmware parses: that a resolvable
	// name produces the marker with rc=0 and an address, and that an
	// unresolvable one still produces the marker — a self-test that stays
	// silent on failure is the silence this whole mechanism exists to end.
	reset();
	stub_naddr = 1;
	gaishim_selftest("host.example.test");
	reset();
	stub_naddr = 0;
	gaishim_selftest("nothing.example.test");
	gaishim_selftest("");                 /* must write nothing at all */
	gaishim_selftest(0);
	printf("(the three lines above stderr: one rc=0 with an ip, one rc!=0, "
	       "and nothing for the empty host)\n");

	/* ── nothing leaked, nothing freed twice ─────────────────────────── */
	ok(live_blocks == 0, "every allocation was freed");

	if (failures) {
		printf("gaishimcheck: %d FAILED\n", failures);
		return 1;
	}
	printf("gaishimcheck: all checks passed\n");
	return 0;
}
