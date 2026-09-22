/*
 * gaishim — getaddrinfo for the endpoints, over the resolver path that works
 * =========================================================================
 *
 * On emOS, bionic's `getaddrinfo` resolves nothing and its `gethostbyname`
 * resolves everything. Both go to `/dev/socket/dnsproxyd`, both are answered
 * by the same `dnsproxy_serve` in `emos/init/init.c`, and only one of them
 * works. librespot, shairport-sync and nqptp all use the broken one, which is
 * the whole of why Spotify Connect and AirPlay are dead on an emOS device
 * (#263).
 *
 * The proxy is not at fault and cannot be fixed from our side. Measured with
 * `device/tools/dnsprobe` (#264) on 2026-09-21: the proxy's reply is netd's
 * serialisation byte for byte, with the right address, and Amazon's bionic
 * rejects it — along with every other shape netd could have sent (eight were
 * tried; with the connection held open bionic BLOCKS, so it wants more per
 * entry than AOSP android-5.1.1_r38 does). Matching netd harder cannot
 * succeed against a parser that wants something netd never writes.
 *
 * So this library answers `getaddrinfo` itself. It is LD_PRELOADed into the
 * endpoints by the firmware, and it resolves names with `gethostbyname` —
 * the call that is measured working on the device. Nothing about Amazon's
 * getaddrinfo wire format has to be known, or guessed, ever again.
 *
 * WHY A PRELOAD AND NOT A FIX IN emOS. emOS ships only inside a boot image
 * the user assembles from their own boot partition, so an emOS fix cannot
 * reach a device that is already running. This is an endpoint binary, on the
 * road librespot and shairport-sync already travel.
 *
 * WHAT IT DELIBERATELY DOES NOT DO. It is not a general resolver. There is no
 * AAAA lookup, because `gethostbyname` on the device answers A records and
 * nothing else — an explicit `AF_INET6` NAME lookup gets EAI_ADDRFAMILY
 * rather than a wrong answer. IPv6 LITERALS and the passive wildcard are
 * answered in full, because those need no lookup at all and a receiver that
 * binds `::` must still be able to.
 *
 * ── The three things that are silent if they are wrong ──────────────────────
 *
 * 1. THE STRUCT LAYOUT. The caller was compiled against Android's `netdb.h`
 *    and we were not, so the field ORDER here is load-bearing and is BSD's,
 *    not glibc's: `ai_canonname` comes BEFORE `ai_addr`. Swapping them
 *    compiles, links, loads and hands every caller a hostname where it wants
 *    a sockaddr. Corroborated twice: the NDK header, and the 32-byte blob
 *    emOS's own proxy serialises (`ai_wire` in init.c).
 *
 * 2. THE THREAD SAFETY OF gethostbyname. It returns a pointer into a buffer
 *    that is reused. There is no lock here, and that is deliberate rather
 *    than forgotten: bionic's buffer is PER THREAD (`__res_get_static`, a
 *    pthread key), unlike glibc's, so the call is safe from librespot's
 *    thread pool. What is NOT safe is holding the pointer, so every address
 *    is copied out before anything else is called.
 *
 * 3. THE ADDRESS FAMILY OF A LITERAL. `inet_pton` is bionic's, not ours,
 *    because it is a pure parser with no network behind it and because
 *    hand-rolling a dotted-quad parser is how `inet_addr`'s legacy forms
 *    ("127.1", "0x7f.1") creep back in and resolve to something plausible.
 *
 * Driven off-target by `gaishimcheck.c`, which includes this file whole and
 * calls the real `getaddrinfo`, for the reason `emos/init`'s checkers exist:
 * every one of the failures above presents on hardware as a program that
 * starts and never connects.
 */

/* ── bionic's ABI, declared rather than included ─────────────────────────────
 *
 * Including <netdb.h> would bind this file to the HOST's layout, which is
 * exactly the thing that must not happen: the checker builds on x86-64 and
 * the library on armv7, and only one of them can be right if the header
 * decides. These declarations are the contract with Android's netdb.h, and
 * `gaishimcheck.c` asserts the parts of it that a compiler can check.
 */

typedef __SIZE_TYPE__  shim_size_t;
typedef unsigned int   shim_socklen_t;
typedef unsigned short shim_uint16_t;
typedef unsigned int   shim_uint32_t;

struct sockaddr;                      /* opaque here; only ever pointed at */

struct addrinfo {
	int               ai_flags;
	int               ai_family;
	int               ai_socktype;
	int               ai_protocol;
	shim_socklen_t    ai_addrlen;
	char             *ai_canonname;   /* BEFORE ai_addr — see note 1 */
	struct sockaddr  *ai_addr;
	struct addrinfo  *ai_next;
};

/* On the device this is 32 bytes: three pointers in a 32-bit process, which
 * is what the caller's netdb.h produces and what emOS's own proxy serialises
 * (`AI_WIRE_LEN` in init.c). A build where it is not is a build whose callers
 * read a different struct than we write, so it must not link. */
typedef char gaishim_addrinfo_is_32_on_target[
    (sizeof(void *) != 4 || sizeof(struct addrinfo) == 32) ? 1 : -1];

struct hostent {
	char  *h_name;
	char **h_aliases;
	int    h_addrtype;
	int    h_length;
	char **h_addr_list;
};

struct servent {
	char  *s_name;
	char **s_aliases;
	int    s_port;                    /* already in network order */
	char  *s_proto;
};

/* Laid out by hand for the same reason as addrinfo. Both are what the kernel
 * reads off a bind() or connect(), so the padding is part of the contract. */
struct shim_sockaddr_in {
	shim_uint16_t sin_family;
	shim_uint16_t sin_port;           /* network order */
	shim_uint32_t sin_addr;           /* network order */
	unsigned char sin_zero[8];
};

struct shim_sockaddr_in6 {
	shim_uint16_t sin6_family;
	shim_uint16_t sin6_port;          /* network order */
	shim_uint32_t sin6_flowinfo;
	unsigned char sin6_addr[16];
	shim_uint32_t sin6_scope_id;
};

#define SHIM_AF_UNSPEC   0
#define SHIM_AF_INET     2
#define SHIM_AF_INET6   10

#define SHIM_SOCK_STREAM 1
#define SHIM_SOCK_DGRAM  2

#define SHIM_IPPROTO_TCP  6
#define SHIM_IPPROTO_UDP 17

#define SHIM_AI_PASSIVE     0x0001
#define SHIM_AI_CANONNAME   0x0002
#define SHIM_AI_NUMERICHOST 0x0004
#define SHIM_AI_NUMERICSERV 0x0008

/* NetBSD's set, which is Android's. */
#define SHIM_EAI_ADDRFAMILY  1
#define SHIM_EAI_BADFLAGS    3
#define SHIM_EAI_FAIL        4
#define SHIM_EAI_FAMILY      5
#define SHIM_EAI_MEMORY      6
#define SHIM_EAI_NODATA      7
#define SHIM_EAI_NONAME      8
#define SHIM_EAI_SERVICE     9

/* ── what we borrow from the process we are loaded into ─────────────────── */

void *malloc(shim_size_t);
void  free(void *);

/* Swapped for stubs by the off-target checker; the real ones on a device. */
#ifndef GAISHIM_RESOLVER
#define GAISHIM_RESOLVER gethostbyname
struct hostent *gethostbyname(const char *);
#endif
#ifndef GAISHIM_SERVICES
#define GAISHIM_SERVICES getservbyname
struct servent *getservbyname(const char *, const char *);
#endif
#ifndef GAISHIM_PTON
#define GAISHIM_PTON inet_pton
int inet_pton(int, const char *, void *);
#endif
#ifndef GAISHIM_RESOLV
#define GAISHIM_RESOLV "/etc/resolv.conf"
#endif

#ifndef GAISHIM_GETENV
#define GAISHIM_GETENV getenv
#define GAISHIM_WRITE  write
char *getenv(const char *);
long  write(int, const void *, shim_size_t);
#endif

/* The socket and file calls the resolver below needs. All ordinary bionic,
 * and all measured working from a 32-bit endpoint process on this device —
 * `socket` and `connect` were the first things ruled out in #263.
 *
 * Declared here rather than included, for this file's usual reason: it must
 * describe the process it is LOADED INTO. The off-target checker builds
 * against the host's headers instead, which is why these are guarded. */
#ifndef GAISHIM_TEST
int  socket(int, int, int);
int  setsockopt(int, int, int, const void *, unsigned int);
long sendto(int, const void *, shim_size_t, int, const void *, unsigned int);
long recv(int, void *, shim_size_t, int);
int  close(int);
int  open(const char *, int, ...);
long read(int, void *, shim_size_t);
#endif

#define SHIM_SOCK_DGRAM_T   2
#define SHIM_SOL_SOCKET     1
#define SHIM_SO_RCVTIMEO   20
#define SHIM_O_RDONLY       0

/* ── small helpers, so the shim imports no string functions ─────────────── */

static int s_len(const char *s)
{
	int n = 0;
	while (s[n])
		n++;
	return n;
}

static void s_copy(char *dst, const char *src, int n)
{
	int i;
	for (i = 0; i < n; i++)
		dst[i] = src[i];
}

static void s_zero(void *p, int n)
{
	unsigned char *b = (unsigned char *)p;
	int i;
	for (i = 0; i < n; i++)
		b[i] = 0;
}

static char s_lower(char c)
{
	return (c >= 'A' && c <= 'Z') ? (char)(c - 'A' + 'a') : c;
}

static int s_ieq(const char *a, const char *b)
{
	while (*a && *b) {
		if (s_lower(*a) != s_lower(*b))
			return 0;
		a++;
		b++;
	}
	return *a == 0 && *b == 0;
}

/* Strictly decimal, and strictly the whole string: a service that is "80x"
 * is not port 80, it is a service name that will not be found. */
static int s_port(const char *s, long *out)
{
	long v = 0;
	if (!s || !*s)
		return -1;
	for (; *s; s++) {
		if (*s < '0' || *s > '9')
			return -1;
		v = v * 10 + (*s - '0');
		if (v > 65535)
			return -1;
	}
	*out = v;
	return 0;
}

/* Host order in, network order out, by building the two bytes and reading
 * them back as the integer — no endianness test, and correct on both. */
static shim_uint16_t hton16(unsigned v)
{
	unsigned char b[2];
	shim_uint16_t out;
	b[0] = (unsigned char)((v >> 8) & 0xff);
	b[1] = (unsigned char)(v & 0xff);
	s_copy((char *)&out, (const char *)b, 2);
	return out;
}

/* ── the resolved set, copied out of anything that can be reused ─────────── */

#define SHIM_MAX_ADDRS 8

struct shim_result {
	int           family;                       /* AF_INET or AF_INET6 */
	int           count;
	unsigned char addr[SHIM_MAX_ADDRS][16];     /* 4 bytes used for v4 */
	char          canon[256];
	int           have_canon;
};

/* ── one entry, allocated with its sockaddr behind it ────────────────────── */

static struct addrinfo *entry_new(int family, const unsigned char *addr,
                                  int socktype, int protocol,
                                  shim_uint16_t port_be)
{
	int addrlen = (family == SHIM_AF_INET6)
	                  ? (int)sizeof(struct shim_sockaddr_in6)
	                  : (int)sizeof(struct shim_sockaddr_in);
	char *block = (char *)malloc(sizeof(struct addrinfo) + (shim_size_t)addrlen);
	struct addrinfo *ai;

	if (!block)
		return 0;
	s_zero(block, (int)sizeof(struct addrinfo) + addrlen);

	ai = (struct addrinfo *)block;
	ai->ai_family   = family;
	ai->ai_socktype = socktype;
	ai->ai_protocol = protocol;
	ai->ai_addrlen  = (shim_socklen_t)addrlen;
	ai->ai_addr     = (struct sockaddr *)(block + sizeof(struct addrinfo));

	if (family == SHIM_AF_INET6) {
		struct shim_sockaddr_in6 *s6 =
		    (struct shim_sockaddr_in6 *)ai->ai_addr;
		s6->sin6_family = SHIM_AF_INET6;
		s6->sin6_port   = port_be;
		s_copy((char *)s6->sin6_addr, (const char *)addr, 16);
	} else {
		struct shim_sockaddr_in *s4 =
		    (struct shim_sockaddr_in *)ai->ai_addr;
		shim_uint32_t v;
		s4->sin_family = SHIM_AF_INET;
		s4->sin_port   = port_be;
		s_copy((char *)&v, (const char *)addr, 4);
		s4->sin_addr = v;
	}
	return ai;
}

/* The two exported symbols. Everything else in this file is hidden, so these
 * attributes ARE the export list — see build.sh, which refuses a library that
 * does not carry both. A preload that exports nothing loads without error and
 * changes nothing. */
#define GAISHIM_EXPORT __attribute__((visibility("default")))

GAISHIM_EXPORT void freeaddrinfo(struct addrinfo *ai)
{
	while (ai) {
		struct addrinfo *next = ai->ai_next;
		if (ai->ai_canonname)
			free(ai->ai_canonname);
		free(ai);
		ai = next;
	}
}

/* ── resolution ─────────────────────────────────────────────────────────── */

/* A literal, in either family. Answered without the proxy, which is why
 * `AI_NUMERICHOST` can be honoured exactly. */
static int lit_parse(const char *node, int want_family, struct shim_result *r)
{
	unsigned char buf[16];

	if (want_family == SHIM_AF_UNSPEC || want_family == SHIM_AF_INET) {
		if (GAISHIM_PTON(SHIM_AF_INET, node, buf) == 1) {
			s_zero(r, (int)sizeof *r);
			r->family = SHIM_AF_INET;
			r->count  = 1;
			s_copy((char *)r->addr[0], (const char *)buf, 4);
			return 0;
		}
	}
	if (want_family == SHIM_AF_UNSPEC || want_family == SHIM_AF_INET6) {
		if (GAISHIM_PTON(SHIM_AF_INET6, node, buf) == 1) {
			s_zero(r, (int)sizeof *r);
			r->family = SHIM_AF_INET6;
			r->count  = 1;
			s_copy((char *)r->addr[0], (const char *)buf, 16);
			return 0;
		}
	}
	return -1;
}

/* `localhost` never reaches the proxy. On emOS it must not: `dnsproxy_serve`
 * has no hosts file, so the name goes upstream and a router that answers
 * NXDOMAIN for it — a FRITZ!Box does — makes loopback unresolvable for every
 * bionic program on the device (#219). nqptp's control port is reached by
 * that exact name, so AirPlay 2 dies on it. Answering here fixes the
 * endpoints without an emOS release; #219 remains the fix for everything
 * else on the device. */
/* 0 = not a loopback name, AF_INET / AF_INET6 = the family the NAME itself
 * carries. Android's own hosts file is the source of the four spellings and
 * of the split: `localhost` is v4 there, `ip6-localhost` is v6. */
static int loopback_name(const char *node)
{
	if (s_ieq(node, "localhost") || s_ieq(node, "localhost.localdomain"))
		return SHIM_AF_INET;
	if (s_ieq(node, "ip6-localhost") || s_ieq(node, "ip6-loopback"))
		return SHIM_AF_INET6;
	return 0;
}

/* The name's family and the caller's both get a say, and they can disagree:
 * `getaddrinfo("localhost", ..., AF_INET6)` must answer ::1, not refuse. */
static void loopback_result(int want_family, int name_family,
                            struct shim_result *r)
{
	int family = (want_family == SHIM_AF_UNSPEC) ? name_family : want_family;

	s_zero(r, (int)sizeof *r);
	r->count = 1;
	if (family == SHIM_AF_INET6) {
		r->family = SHIM_AF_INET6;
		r->addr[0][15] = 1;                    /* ::1 */
	} else {
		r->family = SHIM_AF_INET;
		r->addr[0][0] = 127;
		r->addr[0][3] = 1;                     /* 127.0.0.1 */
	}
}

/* ── resolving without bionic at all ─────────────────────────────────────────
 *
 * The shim was built on `gethostbyname` because that call was measured working
 * where `getaddrinfo` was not. On 2026-09-22 the device answered that it is
 * not enough: the library loads into librespot, is interposed, and its own
 * lookup returns EAI_NODATA — which is what this file returns when
 * `gethostbyname` gives nothing.
 *
 * So the last dependency on Amazon's resolver goes too. emOS already writes
 * `/etc/resolv.conf` and asks the nameserver over UDP itself
 * (`dns_lookup_a` in `emos/init/init.c`); this is the same conversation from
 * inside the endpoint's process. Nothing here touches `/dev/socket/dnsproxyd`,
 * bionic's resolver, or any Amazon code path.
 *
 * THIS IS A SECOND IMPLEMENTATION OF emOS's, and that is a real cost rather
 * than an oversight. The right shape is one implementation in a header both
 * include; it was not done here because `init.c` is PID 1 on a device that is
 * recovered by hand, and refactoring it is not a change to make in the same
 * breath as a fix. When it is extracted, these two go together.
 *
 * `gethostbyname` stays as a FALLBACK, for the case this cannot ask at all —
 * no resolv.conf, or no nameserver in it.
 */

static shim_uint16_t rd16(const unsigned char *p)
{
	return (shim_uint16_t)(((unsigned)p[0] << 8) | p[1]);
}

/* The nameservers, from the file emOS writes. Dotted quads only, which is what
 * it puts there. */
static int dns_servers_from(const char *path, unsigned char out[][4], int max)
{
	char buf[512];
	int fd, n, i = 0, count = 0;

	fd = open(path, SHIM_O_RDONLY);
	if (fd < 0)
		return 0;
	n = (int)read(fd, buf, sizeof buf - 1);
	close(fd);
	if (n <= 0)
		return 0;
	buf[n] = 0;

	while (i < n && count < max) {
		int start = i, j;
		char ip[64];
		int iplen = 0;
		while (i < n && buf[i] != '\n')
			i++;
		buf[i] = 0;
		j = start;
		while (buf[j] == ' ' || buf[j] == '\t')
			j++;
		if (buf[j] == 'n' && buf[j+1] == 'a' && buf[j+2] == 'm' &&
		    buf[j+3] == 'e' && buf[j+4] == 's' && buf[j+5] == 'e' &&
		    buf[j+6] == 'r' && buf[j+7] == 'v' && buf[j+8] == 'e' &&
		    buf[j+9] == 'r') {
			j += 10;
			while (buf[j] == ' ' || buf[j] == '\t')
				j++;
			while (buf[j] && buf[j] != ' ' && buf[j] != '\t' &&
			       iplen < (int)sizeof ip - 1)
				ip[iplen++] = buf[j++];
			ip[iplen] = 0;
			if (iplen && GAISHIM_PTON(SHIM_AF_INET, ip, out[count]) == 1)
				count++;
		}
		i++;
	}
	return count;
}

/* Encode `name` as DNS labels. Returns the message length, or -1 for a name
 * that cannot be encoded — a label over 63 bytes, or one that would not fit.
 * Refusing is right: a truncated name asks about something else and gets a
 * confident answer to the wrong question. */
static int dns_build_query(unsigned char *buf, int cap, const char *name,
                           unsigned id)
{
	int n = 12, lab;
	const char *p = name;

	if (cap < 12)
		return -1;
	s_zero(buf, 12);
	buf[0] = (unsigned char)(id >> 8);
	buf[1] = (unsigned char)(id & 0xff);
	buf[2] = 0x01;                     /* RD; we are not a resolver */
	buf[5] = 1;                        /* QDCOUNT */

	while (*p) {
		const char *dot = p;
		while (*dot && *dot != '.')
			dot++;
		lab = (int)(dot - p);
		if (lab == 0 || lab > 63 || n + lab + 1 > cap - 4)
			return -1;
		buf[n++] = (unsigned char)lab;
		s_copy((char *)buf + n, p, lab);
		n += lab;
		p = *dot ? dot + 1 : dot;
	}
	if (n + 5 > cap)
		return -1;
	buf[n++] = 0;                      /* root label */
	buf[n++] = 0; buf[n++] = 1;        /* QTYPE  A */
	buf[n++] = 0; buf[n++] = 1;        /* QCLASS IN */
	return n;
}

/* Step over one name, following a compression pointer once. Returns the
 * offset after the name, or -1 on anything malformed. */
static int dns_skip_name(const unsigned char *m, int len, int off)
{
	int guard = 0;
	while (off >= 0 && off < len) {
		unsigned c = m[off];
		if (c == 0)
			return off + 1;
		if ((c & 0xc0) == 0xc0)
			return (off + 2 <= len) ? off + 2 : -1;
		off += 1 + (int)c;
		if (++guard > 128)
			return -1;
	}
	return -1;
}

/* A records out of a reply. Returns how many were written, 0 for a
 * well-formed answer with none (NXDOMAIN or no A), -1 for anything we cannot
 * trust. The three are kept apart because the caller must not retry a real
 * answer, and must not accept a malformed one. */
static int dns_parse_a(const unsigned char *m, int len, unsigned id,
                       unsigned char out[][4], int max)
{
	int off, i, count = 0, qd, an;

	if (len < 12)
		return -1;
	if ((((unsigned)m[0] << 8) | m[1]) != id)
		return -1;
	if ((m[2] & 0x80) == 0)            /* not a response */
		return -1;
	if ((m[3] & 0x0f) != 0)            /* RCODE — NXDOMAIN and friends */
		return 0;
	qd = rd16(m + 4);
	an = rd16(m + 6);

	off = 12;
	for (i = 0; i < qd; i++) {
		off = dns_skip_name(m, len, off);
		if (off < 0 || off + 4 > len)
			return -1;
		off += 4;
	}
	for (i = 0; i < an && count < max; i++) {
		int type, rdlen;
		off = dns_skip_name(m, len, off);
		if (off < 0 || off + 10 > len)
			return -1;
		type  = rd16(m + off);
		rdlen = rd16(m + off + 8);
		off += 10;
		if (off + rdlen > len)
			return -1;
		if (type == 1 && rdlen == 4)
			s_copy((char *)out[count++], (const char *)(m + off), 4);
		off += rdlen;
	}
	return count;
}

/* One query per server, twice round. Returns addresses written, 0 for a
 * definitive "no such name", -1 when no server could be asked at all — only
 * the last of those is worth falling back from. */
static int dns_lookup(const char *name, unsigned char out[][4], int max)
{
	unsigned char servers[3][4], q[512], r[1500];
	int ns = dns_servers_from(GAISHIM_RESOLV, servers, 3);
	int qlen, try_, s;
	static unsigned seq;
	unsigned id;

	if (ns == 0)
		return -1;
	/* Enough to reject a stale or spoofed reply on our own socket; this is
	 * a stub resolver on a LAN, not a recursive one. */
	id = (unsigned)((shim_size_t)(void *)&servers ^ (++seq << 3)) & 0xffff;
	qlen = dns_build_query(q, (int)sizeof q, name, id);
	if (qlen < 0)
		return -1;

	for (try_ = 0; try_ < 2; try_++) {
		for (s = 0; s < ns; s++) {
			struct shim_sockaddr_in to;
			int tv[2];
			int fd = socket(SHIM_AF_INET, SHIM_SOCK_DGRAM_T, 0);
			if (fd < 0)
				continue;
			tv[0] = 2; tv[1] = 0;      /* struct timeval, 32-bit target */
			setsockopt(fd, SHIM_SOL_SOCKET, SHIM_SO_RCVTIMEO,
			           tv, (unsigned int)sizeof tv);

			s_zero(&to, (int)sizeof to);
			to.sin_family = SHIM_AF_INET;
			to.sin_port   = hton16(53);
			s_copy((char *)&to.sin_addr, (const char *)servers[s], 4);

			if (sendto(fd, q, (shim_size_t)qlen, 0, (void *)&to,
			           (unsigned int)sizeof to) == qlen) {
				long got = recv(fd, r, sizeof r, 0);
				if (got > 0) {
					int n = dns_parse_a(r, (int)got, id, out, max);
					close(fd);
					if (n > 0)
						return n;
					/* A well-formed NXDOMAIN is an ANSWER. Asking the
					 * next server would produce the same one. */
					if (n == 0)
						return 0;
					continue;
				}
			}
			close(fd);
		}
	}
	return -1;
}

static int name_resolve(const char *node, int want_family, int want_canon,
                        struct shim_result *r)
{
	struct hostent *he;
	int i;

	/* No AAAA path exists, so an explicit v6 NAME lookup is refused rather
	 * than answered with an A record in a v6 sockaddr. */
	if (want_family == SHIM_AF_INET6)
		return SHIM_EAI_ADDRFAMILY;

	/* Ask the nameserver ourselves first. This is the whole point of the
	 * library now: on emOS neither of bionic's two resolver entry points can
	 * be relied on, and the address is one UDP exchange away. */
	{
		unsigned char addrs[SHIM_MAX_ADDRS][4];
		int n = dns_lookup(node, addrs, SHIM_MAX_ADDRS);
		if (n > 0) {
			s_zero(r, (int)sizeof *r);
			r->family = SHIM_AF_INET;
			for (i = 0; i < n; i++)
				s_copy((char *)r->addr[i], (const char *)addrs[i], 4);
			r->count = n;
			if (want_canon) {
				int len = s_len(node);
				if (len > 255)
					len = 255;
				s_copy(r->canon, node, len);
				r->canon[len] = 0;
				r->have_canon = 1;
			}
			return 0;
		}
		/* A definitive "no such name" is an ANSWER and must not be
		 * retried through a second resolver that would give the same
		 * one — or, worse, a different one. */
		if (n == 0)
			return SHIM_EAI_NODATA;
	}

	/* Only when we could not ask at all: no resolv.conf, or nothing in it.
	 * That is the case on a platform that is not emOS, where bionic's own
	 * resolver is the right answer. */
	he = GAISHIM_RESOLVER(node);
	if (!he || !he->h_addr_list || !he->h_addr_list[0])
		return SHIM_EAI_NODATA;
	if (he->h_addrtype != SHIM_AF_INET || he->h_length != 4)
		return SHIM_EAI_ADDRFAMILY;

	s_zero(r, (int)sizeof *r);
	r->family = SHIM_AF_INET;
	for (i = 0; i < SHIM_MAX_ADDRS && he->h_addr_list[i]; i++)
		s_copy((char *)r->addr[i], he->h_addr_list[i], 4);
	r->count = i;

	if (want_canon) {
		const char *n = he->h_name ? he->h_name : node;
		int len = s_len(n);
		if (len > 255)
			len = 255;
		s_copy(r->canon, n, len);
		r->canon[len] = 0;
		r->have_canon = 1;
	}
	return 0;
}

/* ── getaddrinfo ────────────────────────────────────────────────────────── */

GAISHIM_EXPORT int getaddrinfo(const char *node, const char *service,
                               const struct addrinfo *hints,
                               struct addrinfo **res)
{
	int flags = 0, family = SHIM_AF_UNSPEC, socktype = 0, protocol = 0;
	int socktypes[2], nsock = 0, i, s, rc;
	long port = 0;
	shim_uint16_t port_be = 0;
	int port_set = 0;
	struct shim_result r;
	struct addrinfo *head = 0, **tail = &head;

	if (!res)
		return SHIM_EAI_FAIL;
	*res = 0;
	if (!node && !service)
		return SHIM_EAI_NONAME;

	if (hints) {
		flags    = hints->ai_flags;
		family   = hints->ai_family;
		socktype = hints->ai_socktype;
		protocol = hints->ai_protocol;
	}
	if (family != SHIM_AF_UNSPEC && family != SHIM_AF_INET &&
	    family != SHIM_AF_INET6)
		return SHIM_EAI_FAMILY;

	/* ── the port ──────────────────────────────────────────────────────── */
	if (service && *service && s_port(service, &port) != 0) {
		/* Not a number, so it is a service name. `getservbyname` reads
		 * a table compiled into libc and touches no network, so it is
		 * safe on a device whose resolver is the thing being worked
		 * around. */
		struct servent *se;
		const char *proto = (socktype == SHIM_SOCK_DGRAM ||
		                     protocol == SHIM_IPPROTO_UDP)
		                        ? "udp"
		                        : "tcp";
		if (flags & SHIM_AI_NUMERICSERV)
			return SHIM_EAI_SERVICE;
		se = GAISHIM_SERVICES(service, proto);
		if (!se)
			return SHIM_EAI_SERVICE;
		port_be  = (shim_uint16_t)se->s_port;   /* already network order */
		port_set = 1;
	}
	if (!port_set)
		port_be = hton16((unsigned)port);

	/* ── the address ───────────────────────────────────────────────────── */
	if (!node || !*node) {
		/* The passive wildcard and the loopback default both come from
		 * the flags, never from a lookup — a receiver binding a port
		 * must work on a device with no resolver at all. */
		s_zero(&r, (int)sizeof r);
		if (family == SHIM_AF_INET6) {
			r.family = SHIM_AF_INET6;
			r.count  = 1;
			if (!(flags & SHIM_AI_PASSIVE))
				r.addr[0][15] = 1;             /* ::1 */
		} else {
			r.family = SHIM_AF_INET;
			r.count  = 1;
			if (!(flags & SHIM_AI_PASSIVE)) {
				r.addr[0][0] = 127;
				r.addr[0][3] = 1;              /* 127.0.0.1 */
			}
		}
	} else if (lit_parse(node, family, &r) == 0) {
		/* a literal — nothing to look up */
	} else if (flags & SHIM_AI_NUMERICHOST) {
		return SHIM_EAI_NONAME;
	} else if (loopback_name(node)) {
		loopback_result(family, loopback_name(node), &r);
	} else {
		rc = name_resolve(node, family, flags & SHIM_AI_CANONNAME, &r);
		if (rc != 0)
			return rc;
	}
	if (r.count <= 0)
		return SHIM_EAI_NODATA;

	/* ── the socket types ──────────────────────────────────────────────── */
	if (socktype == SHIM_SOCK_STREAM || socktype == SHIM_SOCK_DGRAM) {
		socktypes[nsock++] = socktype;
	} else if (socktype != 0) {
		return SHIM_EAI_SERVICE;
	} else if (protocol == SHIM_IPPROTO_TCP) {
		socktypes[nsock++] = SHIM_SOCK_STREAM;
	} else if (protocol == SHIM_IPPROTO_UDP) {
		socktypes[nsock++] = SHIM_SOCK_DGRAM;
	} else {
		/* Unqualified: both, stream first, which is the order every
		 * caller that takes only the head expects. */
		socktypes[nsock++] = SHIM_SOCK_STREAM;
		socktypes[nsock++] = SHIM_SOCK_DGRAM;
	}

	/* ── the list ──────────────────────────────────────────────────────── */
	for (i = 0; i < r.count; i++) {
		for (s = 0; s < nsock; s++) {
			int proto = protocol ? protocol
			            : (socktypes[s] == SHIM_SOCK_STREAM
			                   ? SHIM_IPPROTO_TCP
			                   : SHIM_IPPROTO_UDP);
			struct addrinfo *ai = entry_new(r.family, r.addr[i],
			                                socktypes[s], proto,
			                                port_be);
			if (!ai) {
				freeaddrinfo(head);
				*res = 0;
				return SHIM_EAI_MEMORY;
			}
			ai->ai_flags = flags;
			*tail = ai;
			tail  = &ai->ai_next;
		}
	}
	if (!head)
		return SHIM_EAI_NODATA;

	/* Exactly one canonical name, on the head, as getaddrinfo(3) says. */
	if (r.have_canon) {
		int len = s_len(r.canon);
		char *c = (char *)malloc((shim_size_t)len + 1);
		if (!c) {
			freeaddrinfo(head);
			*res = 0;
			return SHIM_EAI_MEMORY;
		}
		s_copy(c, r.canon, len);
		c[len] = 0;
		head->ai_canonname = c;
	}

	*res = head;
	return 0;
}


/* ── the self-test ───────────────────────────────────────────────────────────
 *
 * Set `GAISHIM_SELFTEST` to a hostname and this library, at load time, resolves
 * it THROUGH ITS OWN getaddrinfo and writes one line to stderr. Inert
 * otherwise: the variable is unset in normal operation and the constructor
 * returns immediately.
 *
 * # Why it lives here and not in a tool
 *
 * The question that could not be answered from outside on 2026-09-21 is what
 * `getaddrinfo` returns INSIDE an endpoint's own process. The shim is already
 * loaded into exactly that process by exactly the linker under suspicion, so
 * it is the only thing on the device that can answer without a second binary,
 * a second delivery path and a second thing to keep in step.
 *
 * It does NOT prove interposition on its own — it proves what this code
 * returns when the real bionic is underneath it. Read it beside the linker's
 * own lines, which say whether the library was loaded at all.
 */

static void st_puts(const char *s)
{
	int n = 0;
	while (s[n])
		n++;
	GAISHIM_WRITE(2, s, (shim_size_t)n);
}

static void st_num(long v)
{
	char b[24];
	int i = 24;
	int neg = v < 0;
	unsigned long u = neg ? -(unsigned long)v : v;

	if (!u) {
		st_puts("0");
		return;
	}
	while (u) {
		b[--i] = (char)('0' + (u % 10));
		u /= 10;
	}
	if (neg)
		st_puts("-");
	GAISHIM_WRITE(2, b + i, (shim_size_t)(24 - i));
}

/* The marker is matched by the firmware and must not drift. */
#define SELFTEST_MARK "gaishim-selftest: "

void gaishim_selftest(const char *host)
{
	struct addrinfo hints, *res = 0, *ai;
	int rc, n = 0;

	if (!host || !*host)
		return;

	s_zero(&hints, (int)sizeof hints);
	hints.ai_family   = SHIM_AF_UNSPEC;
	hints.ai_socktype = SHIM_SOCK_STREAM;

	st_puts(SELFTEST_MARK);
	st_puts(host);

	/* The RAW answer from the call this library is built on, before our own
	 * logic touches it. Without it, a failure here is ambiguous between "the
	 * device cannot resolve through gethostbyname" and "this file has a bug",
	 * and those want completely different work.
	 *
	 * Measured 2026-09-22: the shim loads into librespot, runs, and returns
	 * EAI_NODATA — which is what our code returns when gethostbyname gives
	 * nothing. Whether it gave nothing is exactly what was not observable. */
	{
		struct hostent *he = GAISHIM_RESOLVER(host);
		st_puts(" ghbn=");
		if (!he) {
			st_puts("null");
		} else if (!he->h_addr_list || !he->h_addr_list[0]) {
			st_puts("empty");
		} else {
			unsigned char *a = (unsigned char *)he->h_addr_list[0];
			int i;
			st_puts("af"); st_num(he->h_addrtype);
			st_puts("/len"); st_num(he->h_length);
			st_puts("/");
			for (i = 0; i < 4; i++) {
				st_num(a[i]);
				if (i < 3) st_puts(".");
			}
		}
	}

	st_puts(" rc=");
	rc = getaddrinfo(host, "443", &hints, &res);
	st_num(rc);

	for (ai = res; ai; ai = ai->ai_next) {
		unsigned char *a;
		int i;
		n++;
		if (ai->ai_family != SHIM_AF_INET || !ai->ai_addr)
			continue;
		a = (unsigned char *)ai->ai_addr + 4;   /* sin_addr */
		st_puts(" ip=");
		for (i = 0; i < 4; i++) {
			st_num(a[i]);
			if (i < 3)
				st_puts(".");
		}
	}
	st_puts(" entries=");
	st_num(n);
	st_puts("\n");
	if (res)
		freeaddrinfo(res);
}

#ifndef GAISHIM_TEST
__attribute__((constructor)) static void gaishim_boot(void)
{
	gaishim_selftest(GAISHIM_GETENV("GAISHIM_SELFTEST"));
}
#endif
