/* mcastnudge — make the kernel re-program the interface's multicast filter,
 * without touching anybody's membership or restarting anything.
 *
 * # The question this exists to answer
 *
 * Inbound multicast on this board stops arriving while every local indicator
 * says it should be (device/CLAUDE.md, "The interface stops accepting the
 * group"). The one repair that reliably works is restarting both responders,
 * and the reason is a SIDE EFFECT: 224.0.0.251's refcount on wlan0 reaches 0
 * and then 1, the kernel's address list changes, and that change is what
 * calls the driver's set_rx_mode and re-programs the chip.
 *
 * The cost of that repair is the whole of the problem with it — it kills a
 * live AirPlay or Spotify session. So: can the same re-program be reached by
 * changing the address list in a way nobody is using?
 *
 * Joining an unrelated group adds a new multicast MAC to the interface's
 * list, which is a change; leaving it is another. Neither goes anywhere near
 * 224.0.0.251, so the responders keep their membership and their sockets, and
 * nothing is restarted. If reception comes back, the mechanism is the
 * re-program rather than the re-join, and the firmware can have a repair that
 * costs nobody their music.
 *
 * **It is an instrument, not a fix.** Nothing in the firmware calls this. Run
 * it on a device that is measurably deaf — `grep '^Udp:' /proc/net/snmp`
 * frozen, the firewall's counter on the 224.0.0.251 rule not moving — and
 * watch both. A result that does not replicate twice, with the fault
 * re-established in between, is what two retracted releases were built on.
 *
 * # Why a group nobody wants
 *
 * The default is 239.255.77.77, administratively scoped (RFC 2365) and not
 * assigned to any protocol, so joining it for half a second asks nothing of
 * anybody else's equipment and receives nothing. It must NOT be 224.0.0.251:
 * the interface already holds that address, so adding it again only bumps a
 * refcount, the address list does not change, and the driver is never called
 * — which would measure nothing while looking like it had.
 *
 * Freestanding, for mdnsprobe's and igmpprobe's reason: it reaches a device
 * as base64 through the controller's shell plane, and linking no libc removes
 * the bionic question rather than answering it.
 */

#define SYS_exit 1
#define SYS_write 4
#define SYS_nanosleep 162
/* ARM EABI has direct socket syscalls rather than i386's socketcall. */
#define SYS_socket 281
#define SYS_setsockopt 294

#define AF_INET 2
#define SOCK_DGRAM 2
#define SOL_IP 0
#define IP_ADD_MEMBERSHIP 35
#define IP_DROP_MEMBERSHIP 36

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

struct ts { long sec; long nsec; };

/* struct ip_mreqn: the group, the local address, and the interface INDEX.
 * The index is what pins this to wlan0 — with ip_mreq's 8-byte form the
 * kernel picks an interface from the route table, and there is no
 * 224.0.0.0/4 route on this device, so it would fall back to the default
 * route's device. True today and not something to rest a measurement on.
 * The kernel tells the two forms apart by the option length. */
struct ip_mreqn {
	unsigned char multiaddr[4];
	unsigned char address[4];
	int ifindex;
};

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

static long parse_num(const char *s) {
	long v = 0;
	for (; *s >= '0' && *s <= '9'; s++) v = v * 10 + (*s - '0');
	return v;
}

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
	struct ip_mreqn mr;
	unsigned char grp[4] = {239, 255, 77, 77};
	long ifindex = 7, cycles = 1, hold_ms = 500;

	if (argc > 1 && !parse_ip(argv[1], grp)) {
		w("usage: mcastnudge [group] [ifindex] [cycles] [hold-ms]\n");
		w("defaults: 239.255.77.77 7 1 500\n");
		w("the interface index is wlan0's, as /proc/net/igmp prints it\n");
		return 2;
	}
	if (argc > 2) ifindex = parse_num(argv[2]);
	if (argc > 3) cycles = parse_num(argv[3]);
	if (argc > 4) hold_ms = parse_num(argv[4]);
	if (cycles < 1) cycles = 1;

	/* 224.0.0.251 is refused rather than silently measuring nothing: the
	 * interface already holds it, so a second join changes no address list
	 * and the driver is never called. */
	if (grp[0] == 224 && grp[1] == 0 && grp[2] == 0 && grp[3] == 251) {
		w("refusing 224.0.0.251: the interface already holds it, so joining\n");
		w("it again changes no address list and re-programs nothing.\n");
		return 2;
	}

	long fd = sys3(SYS_socket, AF_INET, SOCK_DGRAM, 0);
	if (fd < 0) { w("socket failed: "); wn(fd); w("\n"); return 1; }

	for (int i = 0; i < 4; i++) { mr.multiaddr[i] = grp[i]; mr.address[i] = 0; }
	mr.ifindex = (int)ifindex;

	for (long n = 0; n < cycles; n++) {
		long rc = sys5(SYS_setsockopt, fd, SOL_IP, IP_ADD_MEMBERSHIP, (long)&mr, sizeof(mr));
		w("join: ");
		if (rc < 0) { w("failed "); wn(rc); w("\n"); return 1; }
		w("ok\n");

		struct ts t = {hold_ms / 1000, (hold_ms % 1000) * 1000000};
		sys3(SYS_nanosleep, (long)&t, 0, 0);

		rc = sys5(SYS_setsockopt, fd, SOL_IP, IP_DROP_MEMBERSHIP, (long)&mr, sizeof(mr));
		w("leave: ");
		if (rc < 0) { w("failed "); wn(rc); w("\n"); return 1; }
		w("ok\n");

		if (n + 1 < cycles) { struct ts p = {0, 200000000}; sys3(SYS_nanosleep, (long)&p, 0, 0); }
	}
	w("address list changed twice per cycle; read the counters, not this line\n");
	return 0;
}
