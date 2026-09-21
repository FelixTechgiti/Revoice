/*
 * abicheck — the numbers gaishim.c hardcodes, checked against Android's own
 * headers
 * ==========================================================================
 *
 * Compile-only; nothing here runs. `gaishim.c` deliberately includes no
 * system header, because it must describe the ABI of the process it is
 * LOADED INTO rather than of the machine it is BUILT ON — and the checker
 * that drives it runs on an x86-64 host where every one of these numbers is
 * different. That leaves the constants themselves unpinned, and each of them
 * fails silently: a wrong `AF_INET6` refuses a family nobody asked about, a
 * wrong `EAI_NODATA` is read by the caller as a different failure, a wrong
 * field offset hands a caller a `char *` where it wants a `sockaddr`.
 *
 * So this file includes the real Android headers, for the real target, and
 * asserts the numbers. It is compiled by build.sh whenever an NDK is present.
 */

#include <netdb.h>
#include <stddef.h>
#include <sys/socket.h>
#include <netinet/in.h>

#define CHECK(name, cond) typedef char abicheck_##name[(cond) ? 1 : -1]

/* The struct the caller passes and reads back. BSD order: ai_canonname
 * BEFORE ai_addr. */
CHECK(ai_flags_at_0,      offsetof(struct addrinfo, ai_flags)     == 0);
CHECK(ai_family_at_4,     offsetof(struct addrinfo, ai_family)    == 4);
CHECK(ai_socktype_at_8,   offsetof(struct addrinfo, ai_socktype)  == 8);
CHECK(ai_protocol_at_12,  offsetof(struct addrinfo, ai_protocol)  == 12);
CHECK(ai_addrlen_at_16,   offsetof(struct addrinfo, ai_addrlen)   == 16);
CHECK(ai_canonname_at_20, offsetof(struct addrinfo, ai_canonname) == 20);
CHECK(ai_addr_at_24,      offsetof(struct addrinfo, ai_addr)      == 24);
CHECK(ai_next_at_28,      offsetof(struct addrinfo, ai_next)      == 28);
CHECK(addrinfo_is_32,     sizeof(struct addrinfo)                 == 32);

/* What gethostbyname hands back. */
CHECK(h_name_at_0,       offsetof(struct hostent, h_name)      == 0);
CHECK(h_aliases_at_4,    offsetof(struct hostent, h_aliases)   == 4);
CHECK(h_addrtype_at_8,   offsetof(struct hostent, h_addrtype)  == 8);
CHECK(h_length_at_12,    offsetof(struct hostent, h_length)    == 12);
CHECK(h_addr_list_at_16, offsetof(struct hostent, h_addr_list) == 16);

/* s_port is the one field read out of a servent, and it is already in
 * network order — the shim must not swap it a second time. */
CHECK(s_port_at_8, offsetof(struct servent, s_port) == 8);

/* What the kernel reads off a bind() or connect(). */
CHECK(sockaddr_in_is_16,  sizeof(struct sockaddr_in)  == 16);
CHECK(sockaddr_in6_is_28, sizeof(struct sockaddr_in6) == 28);
CHECK(sin_port_at_2,      offsetof(struct sockaddr_in, sin_port)  == 2);
CHECK(sin_addr_at_4,      offsetof(struct sockaddr_in, sin_addr)  == 4);
CHECK(sin6_port_at_2,     offsetof(struct sockaddr_in6, sin6_port) == 2);
CHECK(sin6_addr_at_8,     offsetof(struct sockaddr_in6, sin6_addr) == 8);

CHECK(af_inet_is_2,     AF_INET     == 2);
CHECK(af_inet6_is_10,   AF_INET6    == 10);
CHECK(af_unspec_is_0,   AF_UNSPEC   == 0);
CHECK(sock_stream_is_1, SOCK_STREAM == 1);
CHECK(sock_dgram_is_2,  SOCK_DGRAM  == 2);
CHECK(ipproto_tcp_is_6,  IPPROTO_TCP == 6);
CHECK(ipproto_udp_is_17, IPPROTO_UDP == 17);

CHECK(ai_passive_is_1,     AI_PASSIVE     == 0x0001);
CHECK(ai_canonname_is_2,   AI_CANONNAME   == 0x0002);
CHECK(ai_numerichost_is_4, AI_NUMERICHOST == 0x0004);
CHECK(ai_numericserv_is_8, AI_NUMERICSERV == 0x0008);

CHECK(eai_addrfamily_is_1, EAI_ADDRFAMILY == 1);
CHECK(eai_badflags_is_3,   EAI_BADFLAGS   == 3);
CHECK(eai_fail_is_4,       EAI_FAIL       == 4);
CHECK(eai_family_is_5,     EAI_FAMILY     == 5);
CHECK(eai_memory_is_6,     EAI_MEMORY     == 6);
CHECK(eai_nodata_is_7,     EAI_NODATA     == 7);
CHECK(eai_noname_is_8,     EAI_NONAME     == 8);
CHECK(eai_service_is_9,    EAI_SERVICE    == 9);
