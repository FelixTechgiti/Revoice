typedef unsigned long size_t;
long write(int, const void *, size_t);
int getaddrinfo(const char *, const char *, const void *, void **);
void freeaddrinfo(void *);
void *gethostbyname(const char *);
void _exit(int);

static int slen(const char *s){int n=0;while(s[n])n++;return n;}
static void w(const char *s){write(1,s,slen(s));}
static void wn(long v){char b[24];int i=24;int neg=v<0;unsigned long u=neg?-(unsigned long)v:v;
  if(!u){w("0");return;} while(u){b[--i]='0'+(char)(u%10);u/=10;}
  if(neg)w("-"); write(1,b+i,24-i);}

struct ai { int flags, family, socktype, protocol; unsigned addrlen;
            char *canonname; unsigned char *addr; struct ai *next; };

static void try(const char *label, const char *host, const char *serv, int *hints)
{
    void *res = 0;
    w(label); w(": ");
    int rc = getaddrinfo(host, serv, hints, &res);
    w("rc="); wn(rc);
    if (rc == 0) {
        int n = 0;
        for (struct ai *a = res; a; a = a->next) {
            n++;
            w("\n    fam="); wn(a->family); w(" sock="); wn(a->socktype);
            w(" proto="); wn(a->protocol); w(" addrlen="); wn(a->addrlen);
            if (a->addr) { w(" ip=");
                for (int i = 4; i < 8; i++) { wn(a->addr[i]); if (i<7) w("."); } }
        }
        w("\n  entries="); wn(n);
        freeaddrinfo(res);
    }
    w("\n");
}

__attribute__((constructor)) static void run(void)
{
    const char *h = "clienttoken.spotify.com";
    w("sizeof(struct addrinfo) as this build sees it = "); wn(sizeof(struct ai)); w("\n");
    try("hints=NULL        ", h, "443", 0);
    int h1[8] = {0,0,1,0,0,0,0,0};      /* AF_UNSPEC, SOCK_STREAM */
    try("UNSPEC/STREAM     ", h, "443", h1);
    int h2[8] = {0,2,1,0,0,0,0,0};      /* AF_INET, SOCK_STREAM */
    try("INET/STREAM       ", h, "443", h2);
    void *he = gethostbyname(h);
    w("gethostbyname: "); wn((long)(he != 0)); w("\n");
    _exit(0);
}
