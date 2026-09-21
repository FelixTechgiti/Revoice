/* dnsprobe — ask emOS's DNS proxy the two questions bionic asks, and print the
 * bytes that come back.
 *
 * getaddrinfo and gethostbyname are two protocols on one socket, and on a
 * device they fail the same way: EAI_NODATA, no log at either end, a program
 * that says only that a name did not resolve. Nothing on the device separates
 * "the proxy answered wrongly" from "the proxy was never asked" — the same
 * silence dnscheck.c exists for, except dnscheck drives the function directly
 * and therefore cannot see whether anything reaches it.
 *
 * Freestanding for mdnsprobe's reasons: it crosses the shell plane as base64,
 * and depending on no libc removes the question of which libc, on a box where
 * the libc under test is the thing being measured.
 */

#define SYS_exit 1
#define SYS_read 3
#define SYS_write 4
#define SYS_close 6
/* ARM EABI has direct socket syscalls rather than i386's socketcall. */
#define SYS_socket 281
#define SYS_connect 283
#define SYS_setsockopt 294

static inline long sys3(long n, long a, long b, long c) {
	register long r7 __asm__("r7") = n;
	register long r0 __asm__("r0") = a;
	register long r1 __asm__("r1") = b;
	register long r2 __asm__("r2") = c;
	__asm__ volatile("svc 0" : "+r"(r0) : "r"(r7), "r"(r1), "r"(r2) : "memory");
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

/* Divide by ten by hand: ARM has no hardware division, and libgcc's
 * __aeabi_uidivmod drags in `raise`, which needs the libc this does not link. */
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
static void whex(unsigned char c) {
	static const char h[] = "0123456789abcdef";
	char b[2]; b[0] = h[c >> 4]; b[1] = h[c & 15];
	wr(b, 2);
}

struct sa_un { unsigned short fam; char path[108]; };
struct tv { long sec; long usec; };

static char req[512];
static unsigned char rep[4096];

/* Append to req at *n, NUL not included. */
static void app(int *n, const char *s) {
	while (*s && *n < (int)sizeof req - 1) req[(*n)++] = *s++;
}

/* One request on one connection, exactly as bionic makes it: the line, then a
 * literal NUL, then read until the far end closes. Returns bytes read, or a
 * negative errno from whichever call failed first. */
static int ask(const char *line, int len, unsigned char *out, int outsz) {
	int s = sys3(SYS_socket, 1 /*AF_UNIX*/, 1 /*SOCK_STREAM*/, 0);
	if (s < 0) { w("  socket failed, errno "); wn(-s); w("\n"); return s; }

	struct sa_un a;
	for (unsigned i = 0; i < sizeof a; i++) ((char *)&a)[i] = 0;
	a.fam = 1;
	const char *p = "/dev/socket/dnsproxyd";
	for (int i = 0; p[i]; i++) a.path[i] = p[i];

	long rc = sys3(SYS_connect, s, (long)&a, sizeof a);
	if (rc < 0) {
		w("  connect failed, errno "); wn(-rc); w("\n");
		sys3(SYS_close, s, 0, 0);
		return rc;
	}

	/* Bounded, or a proxy that accepts and never answers hangs the probe —
	 * and "hung" is one of the answers this is here to distinguish. */
	struct tv t; t.sec = 10; t.usec = 0;
	sys6(SYS_setsockopt, s, 1 /*SOL_SOCKET*/, 20 /*SO_RCVTIMEO*/,
	     (long)&t, sizeof t, 0);

	/* len+1: the NUL is part of the request, not a terminator we drop. */
	rc = sys3(SYS_write, s, (long)line, len + 1);
	if (rc != len + 1) {
		w("  write returned "); wn(rc); w("\n");
		sys3(SYS_close, s, 0, 0);
		return rc < 0 ? (int)rc : -1;
	}

	int got = 0;
	for (;;) {
		long n = sys3(SYS_read, s, (long)(out + got), outsz - got);
		if (n == 0) break;                      /* far end closed */
		if (n < 0) {
			w("  read returned errno "); wn(-n);
			w(" after "); wn(got); w(" bytes\n");
			break;
		}
		got += (int)n;
		if (got >= outsz) break;
	}
	sys3(SYS_close, s, 0, 0);
	return got;
}

static void dump(const unsigned char *b, int n) {
	for (int i = 0; i < n; i++) {
		if (i && !(i % 16)) w("\n    ");
		else if (i) w(" ");
		whex(b[i]);
	}
	w("\n");
}

static void probe(const char *label, const char *pre, const char *host,
                  const char *post) {
	int n = 0;
	app(&n, pre); app(&n, host); app(&n, post);
	req[n] = 0;

	w(label); w("\n  request: "); wr(req, n); w("\n");
	int got = ask(req, n, rep, sizeof rep);
	if (got < 0) return;
	w("  reply: "); wn(got); w(" bytes");
	if (got >= 4) {
		w(", code "); wr((const char *)rep, 4);
	}
	w("\n");
	if (got > 0) { w("    "); dump(rep, got); }
	if (got == 0) w("    nothing: the proxy closed without answering\n");
}

void _start_c(int argc, char **argv) {
	/* A whole request line as one argument sends exactly that, which is how a
	 * shape bionic produces gets reproduced without rebuilding: the request is
	 * the variable being tested, so it has to be settable. */
	if (argc > 2 && argv[1][0] == '-' && argv[1][1] == 'r') {
		int n = 0;
		app(&n, argv[2]);
		req[n] = 0;
		w("raw\n  request: "); wr(req, n); w("\n");
		int got = ask(req, n, rep, sizeof rep);
		if (got >= 0) {
			w("  reply: "); wn(got); w(" bytes");
			if (got >= 4) { w(", code "); wr((const char *)rep, 4); }
			w("\n");
			if (got > 0) { w("    "); dump(rep, got); }
			else w("    nothing: the proxy closed without answering\n");
		}
		sys3(SYS_exit, 0, 0, 0);
	}

	const char *host = argc > 1 ? argv[1] : "apresolve.spotify.com";

	w("host: "); w(host); w("\n\n");
	/* The two lines are bionic's own, from libc.so's format strings:
	 *   getaddrinfo %s %s %d %d %d %d %u
	 *   gethostbyname %u %s %d
	 * The getaddrinfo arguments are what busybox's hints produce — no flags,
	 * AF_UNSPEC, SOCK_STREAM, no protocol, netid 0. */
	probe("getaddrinfo", "getaddrinfo ", host, " 0 0 0 1 0 0");
	w("\n");
	probe("gethostbyname", "gethostbyname 0 ", host, " 2");

	sys3(SYS_exit, 0, 0, 0);
}

__asm__(
	".global _start\n"
	"_start:\n"
	"  ldr r0, [sp]\n"           /* argc */
	"  add r1, sp, #4\n"         /* argv */
	"  bl _start_c\n"
	"  mov r7, #1\n"
	"  svc 0\n"
);
