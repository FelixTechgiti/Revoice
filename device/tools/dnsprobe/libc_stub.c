long write(int a,const void*b,unsigned long c){return a;}
long read(int a,void*b,unsigned long c){return a;}
int close(int a){return a;}
int unlink(const char*a){return 0;}
int fork(void){return 0;}
int getaddrinfo(const char*a,const char*b,const void*c,void**d){return 0;}
void freeaddrinfo(void*a){}
void _exit(int a){for(;;);}
int usleep(unsigned a){return 0;}
int connect(int a,const void*b,unsigned c){return a;}
