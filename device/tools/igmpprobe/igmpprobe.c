/* igmpprobe — re-assert an IGMP group membership from the device, by hand.
 *
 * Linux emits an unsolicited membership report only when a group's refcount
 * on an interface goes 0 -> 1. Revoice holds 224.0.0.251 continuously with
 * two long-lived responder sockets (librespot's libmdns and shairport-sync's
 * tinysvcmdns), so after boot the membership is never re-asserted. Where the
 * access point ages its snooping entry out and no querier ever asks, inbound
 * multicast stops and cannot come back: the IGMP General Query that would
 * prompt a fresh report is itself multicast.
 *
 * This sends the report the kernel will not, without touching either
 * responder. It is the instrument that proves the mechanism; the firmware
 * does the same thing on a timer (internal/mcastkeep).
 *
 * Freestanding, for mdnsprobe's reasons: it crosses the shell plane as
 * base64, and depending on no libc removes the bionic question rather than
 * answering it.
 */

#define SYS_exit 1
#define SYS_write 4
#define SYS_nanosleep 162
/* ARM EABI has direct socket syscalls rather than i386's socketcall. */
#define SYS_socket 281
#define SYS_sendto 290
#define SYS_setsockopt 294

#define AF_INET 2
#define SOCK_RAW 3
#define IPPROTO_IGMP 2
#define SOL_SOCKET 1
#define SO_BINDTODEVICE 25
#define SOL_IP 0
#define IP_HDRINCL 3

static inline long sys3(long n, long a, long b, long c) {
	register long r7 __asm__("r7") = n;
	register long r0 __asm__("r0") = a;
	register long r1 __asm__("r1") = b;
	register long r2 __asm__("r2") = c;
	__asm__ volatile("svc 0" : "+r"(r0) : "r"(r7), "r"(r1), "r"(r2) : "memory");
	return r0;
}

static inline long sys5(long n, long a, long b, long c, long d, long e) {
	register long r7 __asm__("r7") = n;
	register long r0 __asm__("r0") = a;
	register long r1 __asm__("r1") = b;
	register long r2 __asm__("r2") = c;
	register long r3 __asm__("r3") = d;
	register long r4 __asm__("r4") = e;
	__asm__ volatile("svc 0" : "+r"(r0)
		: "r"(r7), "r"(r1), "r"(r2), "r"(r3), "r"(r4) : "memory");
	return r0;
}

static inline long sys6(long n, long a, long b, long c, long d, long e, long f) {
	register long r7 __asm__("r7") = n;
	register long r0 __asm__("r0") = a;
	register long r1 __asm__("r1") = b;
	register long r2 __asm__("r2") = c;
	register long r3 __asm__("r3") = d;
	register long r4 __asm__("r4") = e;
	register long r5 __asm__("r5") = f;
	__asm__ volatile("svc 0" : "+r"(r0)
		: "r"(r7), "r"(r1), "r"(r2), "r"(r3), "r"(r4), "r"(r5) : "memory");
	return r0;
}

static void wr(const char *s, int n) { sys3(SYS_write, 1, (long)s, n); }
static int slen(const char *s) { int n = 0; while (s[n]) n++; return n; }
static void w(const char *s) { wr(s, slen(s)); }

/* Divide by ten by hand: ARM has no hardware division, so `v / 10` emits a
 * call to __aeabi_uidivmod, whose libgcc copy drags in `raise` — a libc
 * symbol this binary deliberately does not have. */
static unsigned long div10(unsigned long v, unsigned long *rem) {
	unsigned long q = 0, bit = 1, d = 10;
	while (d <= v && !(d & 0x80000000UL)) { d <<= 1; bit <<= 1; }
	while (bit) {
		if (v >= d) { v -= d; q |= bit; }
		d >>= 1; bit >>= 1;
	}
	*rem = v;
	return q;
}

static void wn(long sv) {
	unsigned long v;
	char b[24]; int i = 24;
	if (sv < 0) { w("-"); v = (unsigned long)(-sv); } else v = (unsigned long)sv;
	if (!v) { w("0"); return; }
	while (v) { unsigned long r; v = div10(v, &r); b[--i] = '0' + (char)r; }
	wr(b + i, 24 - i);
}

struct sa_in { unsigned short fam; unsigned short port; unsigned char addr[4]; unsigned char pad[8]; };
struct ts { long sec; long nsec; };

/* The one-complement sum every IP-family checksum uses. The IGMP message is
 * always an even number of bytes here, so there is no odd-length tail. */
static unsigned short cksum(const unsigned char *p, int n) {
	unsigned long s = 0;
	for (int i = 0; i + 1 < n; i += 2) s += ((unsigned long)p[i] << 8) | p[i + 1];
	while (s >> 16) s = (s & 0xFFFF) + (s >> 16);
	s = ~s & 0xFFFF;
	return (unsigned short)s;
}

/* An IP header carrying the Router Alert option (RFC 2113), which RFC 3376
 * requires on every IGMP message and which is what a snooping switch looks
 * for. IHL is therefore 6 words, not 5.
 *
 * The kernel overwrites tot_len, the header checksum and a zero id under
 * IP_HDRINCL, and fills a zero source address with the route's own — which
 * is wanted here, so that this cannot disagree with the address the
 * responders advertise. */
static int build_ip(unsigned char *o, const unsigned char *dst, int payload) {
	o[0] = 0x46;          /* version 4, IHL 6 */
	o[1] = 0xC0;          /* DSCP CS6, as RFC 3376 asks for */
	o[2] = 0; o[3] = (unsigned char)(24 + payload);
	o[4] = 0; o[5] = 0;   /* id: the kernel picks one */
	o[6] = 0; o[7] = 0;   /* no fragmentation */
	o[8] = 1;             /* TTL 1 — IGMP never leaves the link */
	o[9] = IPPROTO_IGMP;
	o[10] = 0; o[11] = 0; /* checksum: the kernel computes it */
	o[12] = 0; o[13] = 0; o[14] = 0; o[15] = 0;   /* source: the kernel fills it */
	for (int i = 0; i < 4; i++) o[16 + i] = dst[i];
	o[20] = 0x94; o[21] = 0x04; o[22] = 0; o[23] = 0;  /* Router Alert */
	return 24 + payload;
}

/* IGMPv2 Membership Report, sent to the group itself. */
static int build_v2(unsigned char *o, const unsigned char *grp) {
	unsigned char *m = o + 24;
	m[0] = 0x16; m[1] = 0;
	m[2] = 0; m[3] = 0;
	for (int i = 0; i < 4; i++) m[4 + i] = grp[i];
	unsigned short c = cksum(m, 8);
	m[2] = (unsigned char)(c >> 8); m[3] = (unsigned char)(c & 0xFF);
	return build_ip(o, grp, 8);
}

/* IGMPv3 Membership Report, sent to 224.0.0.22, carrying one group record.
 *
 * The record type is MODE_IS_EXCLUDE (2) — a CURRENT-STATE report, which is
 * exactly what the kernel would send in answer to a General Query. A
 * state-change type would claim a transition that is not happening, and a
 * querier that believes it may act on the claim rather than on the state. */
static int build_v3(unsigned char *o, const unsigned char *grp) {
	static const unsigned char allv3[4] = {224, 0, 0, 22};
	unsigned char *m = o + 24;
	m[0] = 0x22; m[1] = 0;
	m[2] = 0; m[3] = 0;
	m[4] = 0; m[5] = 0;
	m[6] = 0; m[7] = 1;   /* one group record */
	m[8] = 2;             /* MODE_IS_EXCLUDE */
	m[9] = 0;             /* no auxiliary data */
	m[10] = 0; m[11] = 0; /* no sources: this is an any-source membership */
	for (int i = 0; i < 4; i++) m[12 + i] = grp[i];
	unsigned short c = cksum(m, 16);
	m[2] = (unsigned char)(c >> 8); m[3] = (unsigned char)(c & 0xFF);
	return build_ip(o, allv3, 16);
}

static int parse_ip(const char *s, unsigned char *out) {
	int part = 0, v = 0, digits = 0;
	for (;; s++) {
		if (*s >= '0' && *s <= '9') { v = v * 10 + (*s - '0'); digits++; if (v > 255) return 0; }
		else if (*s == '.' || *s == 0) {
			if (!digits || part > 3) return 0;
			out[part++] = (unsigned char)v;
			v = 0; digits = 0;
			if (*s == 0) break;
		} else return 0;
	}
	return part == 4;
}

static unsigned char pkt[64];

int main(int argc, char **argv);

/* No libc means no crt0, so this is the ELF entry point. The ARM EABI puts
 * argc at [sp] and argv immediately after it. */
__asm__(
	".global _start\n"
	"_start:\n"
	"  ldr r0, [sp]\n"
	"  add r1, sp, #4\n"
	"  bl main\n"
	"  mov r7, #1\n"
	"  svc 0\n");

int main(int argc, char **argv) {
	unsigned char grp[4] = {224, 0, 0, 251};
	const char *iface = "wlan0";
	long count = 1, every = 0;

	if (argc > 1 && !parse_ip(argv[1], grp)) {
		w("usage: igmpprobe [group] [count] [interval-seconds] [interface]\n");
		return 2;
	}
	if (argc > 2) { count = 0; for (const char *p = argv[2]; *p >= '0' && *p <= '9'; p++) count = count * 10 + (*p - '0'); }
	if (argc > 3) { every = 0; for (const char *p = argv[3]; *p >= '0' && *p <= '9'; p++) every = every * 10 + (*p - '0'); }
	if (argc > 4) iface = argv[4];
	if (count < 1) count = 1;

	long fd = sys3(SYS_socket, AF_INET, SOCK_RAW, IPPROTO_IGMP);
	if (fd < 0) { w("socket failed: "); wn(fd); w(" (raw sockets need root)\n"); return 1; }

	/* Bind to the interface rather than trusting the route. There is no
	 * 224.0.0.0/4 route on this device, so a multicast send falls back to
	 * the default route's device — true today and not something to rest on. */
	if (sys5(SYS_setsockopt, fd, SOL_SOCKET, SO_BINDTODEVICE, (long)iface, slen(iface) + 1) < 0) {
		w("SO_BINDTODEVICE "); w(iface); w(" failed\n"); return 1;
	}
	int one = 1;
	if (sys5(SYS_setsockopt, fd, SOL_IP, IP_HDRINCL, (long)&one, 4) < 0) {
		w("IP_HDRINCL failed\n"); return 1;
	}

	for (long n = 0; n < count; n++) {
		if (n && every > 0) { struct ts t = {every, 0}; sys3(SYS_nanosleep, (long)&t, 0, 0); }

		struct sa_in to;
		int len;
		long rc;

		len = build_v2(pkt, grp);
		to.fam = AF_INET; to.port = 0;
		for (int i = 0; i < 4; i++) to.addr[i] = pkt[16 + i];
		rc = sys6(SYS_sendto, fd, (long)pkt, len, 0, (long)&to, sizeof(to));
		w("v2 report -> group: "); if (rc < 0) { w("failed "); wn(rc); } else { wn(rc); w(" bytes"); } w("\n");

		len = build_v3(pkt, grp);
		to.fam = AF_INET; to.port = 0;
		for (int i = 0; i < 4; i++) to.addr[i] = pkt[16 + i];
		rc = sys6(SYS_sendto, fd, (long)pkt, len, 0, (long)&to, sizeof(to));
		w("v3 report -> 224.0.0.22: "); if (rc < 0) { w("failed "); wn(rc); } else { wn(rc); w(" bytes"); } w("\n");
	}
	return 0;
}
