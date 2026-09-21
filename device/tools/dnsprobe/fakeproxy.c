/* fakeproxy — stand in for emOS's DNS proxy for as long as a measurement takes.
 *
 * bionic rejects the real proxy's getaddrinfo reply and says only EAI_NODATA,
 * so the only way to learn WHICH byte it objects to is to vary the reply. The
 * socket path is fixed in libc, so the real one is renamed aside and put back;
 * a unix socket's listener is bound to the inode, not the name, so init's
 * proxy survives the swap untouched.
 *
 *   fakeproxy <addrinfo_len> <connections>
 */
#define SYS_exit 1
#define SYS_fork 2
#define SYS_read 3
#define SYS_write 4
#define SYS_close 6
#define SYS_unlink 10
#define SYS_chmod 15
#define SYS_open 5
#define SYS_socket 281
#define SYS_bind 282
#define SYS_listen 284
#define SYS_accept 285

static inline long sys3(long n, long a, long b, long c) {
	register long r7 __asm__("r7") = n; register long r0 __asm__("r0") = a;
	register long r1 __asm__("r1") = b; register long r2 __asm__("r2") = c;
	__asm__ volatile("svc 0" : "+r"(r0) : "r"(r7), "r"(r1), "r"(r2) : "memory");
	return r0;
}
static int slen(const char *s){int n=0;while(s[n])n++;return n;}
static void w(const char *s){sys3(SYS_write,1,(long)s,slen(s));}
static void zero(void *p,int n){char *c=p;while(n--)*c++=0;}
static void cp(char *d,const char *s){while((*d++=*s++));}
static int atoi_(const char *s){int v=0;while(*s>='0'&&*s<='9'){v=v*10+(*s-'0');s++;}return v;}
static void be32(unsigned char *p,unsigned v){p[0]=v>>24;p[1]=v>>16;p[2]=v>>8;p[3]=v;}
static void put32(unsigned char *p,int v){p[0]=v;p[1]=v>>8;p[2]=v>>16;p[3]=v>>24;}

struct sa_un { unsigned short fam; char path[108]; };
#define SOCKPATH "/dev/socket/dnsproxyd"

void _start_c(int argc, char **argv)
{
	int mode = argc > 1 ? atoi_(argv[1]) : 0;
	int conns = argc > 2 ? atoi_(argv[2]) : 4;

	int lfd = sys3(SYS_socket, 1, 1, 0);
	struct sa_un a; zero(&a, sizeof a); a.fam = 1; cp(a.path, SOCKPATH);
	if (sys3(SYS_bind, lfd, (long)&a, sizeof a) < 0) { w("bind failed\n"); sys3(SYS_exit,1,0,0); }
	sys3(SYS_listen, lfd, 8, 0);
	sys3(SYS_chmod, (long)SOCKPATH, 0666, 0);
	w("fakeproxy up\n");

	long pid = sys3(SYS_fork, 0, 0, 0);
	if (pid != 0) sys3(SYS_exit, 0, 0, 0);
	sys3(SYS_close, 0, 0, 0); sys3(SYS_close, 1, 0, 0); sys3(SYS_close, 2, 0, 0);

	for (int i = 0; i < conns; i++) {
		int c = sys3(SYS_accept, lfd, 0, 0);
		if (c < 0) break;
		char cmd[512]; int k = 0;
		while (k < 511) { char ch; if (sys3(SYS_read, c, (long)&ch, 1) != 1) break;
		                  if (!ch) break; cmd[k++] = ch; }
		cmd[k++] = '\n';
		int lf = sys3(SYS_open, (long)"/data/local/tmp/fp.log", 1|64|1024, 0644);
		if (lf >= 0) { sys3(SYS_write, lf, (long)cmd, k); sys3(SYS_close, lf, 0, 0); }
		unsigned char r[1024]; int n = 0;
		int ailen = 32, canon = 0, term = 1, ptrs = 0, salen = 16, entries = 1;
		int extra = 0, order = 0;
		switch (mode) {
		case 0: break;                       /* netd's shape, as emOS sends it */
		case 1: ailen = 48; break;           /* what a 64-bit netd would send */
		case 2: ailen = 128; break;
		case 3: canon = 1; break;            /* a canonical name, NUL included */
		case 4: term = 0; break;             /* no end-of-list, just close */
		case 5: ptrs = 1; break;             /* netd leaves its own pointers in */
		case 6: salen = 128; break;          /* a whole sockaddr_storage */
		case 7: entries = 2; break;
		case 8: break;                       /* reply, then hold the connection */
		case 9: entries = 0; term = 0; break;/* no reply at all, hold */
		case 10: extra = 1; break;           /* one more empty field per entry */
		case 11: extra = 2; break;
		case 12: extra = 3; break;
		case 13: order = 1; break;           /* canonical name before sockaddr */
		}
		r[n++]='2'; r[n++]='2'; r[n++]='2'; r[n++]=' ';
		for (int e = 0; e < entries; e++) {
			be32(r+n, ailen); n += 4;
			unsigned char ai[160]; zero(ai, 160);
			put32(ai+0, 0); put32(ai+4, 2); put32(ai+8, 1); put32(ai+12, 6);
			put32(ai+16, salen == 128 ? 16 : salen);
			if (ptrs) { put32(ai+20, 0xdead0001); put32(ai+24, 0xdead0002);
			            put32(ai+28, 0xdead0003); }
			for (int j = 0; j < ailen; j++) r[n++] = ai[j];
			unsigned char sa[128]; zero(sa, 128);
			sa[0]=2; sa[1]=0; sa[2]=1; sa[3]=0xbb;
			sa[4]=35; sa[5]=186; sa[6]=224; sa[7]=24;
			if (order) { be32(r+n, 0); n += 4; }        /* canonical name first */
			be32(r+n, salen); n += 4;
			for (int j = 0; j < salen; j++) r[n++] = sa[j];
			if (!order) {
				if (canon) { be32(r+n, 2); n += 4; r[n++]='x'; r[n++]=0; }
				else { be32(r+n, 0); n += 4; }
			}
			for (int j = 0; j < extra; j++) { be32(r+n, 0); n += 4; }
		}
		if (term) { be32(r+n, 0); n += 4; }
		if (mode != 9)
			sys3(SYS_write, c, (long)r, n);
		if (mode >= 8) {
			/* Hold the connection open. A caller that is READING blocks here;
			 * one that already gave up returns at once, and that difference is
			 * the whole measurement. */
			char b;
			sys3(SYS_read, c, (long)&b, 1);
		}
		sys3(SYS_close, c, 0, 0);
	}
	sys3(SYS_unlink, (long)SOCKPATH, 0, 0);
	sys3(SYS_exit, 0, 0, 0);
}

__asm__(".global _start\n_start:\n  ldr r0, [sp]\n  add r1, sp, #4\n  bl _start_c\n  mov r7, #1\n  svc 0\n");
