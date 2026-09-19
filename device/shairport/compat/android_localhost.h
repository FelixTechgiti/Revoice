/*
 * android_localhost.h — declarations for the getaddrinfo shim. See
 * android_localhost.c.
 *
 * Its own header rather than a line in android_compat.h, for that file's own
 * reason: this one is needed by **nqptp too**, which is a different program
 * with its own build, and the two AirPlay 2 binaries are the only consumers
 * either way.
 *
 * The rename to the standard name is guarded on __ANDROID__ so the same
 * sources compile on the host, where localhost resolves and localhostcheck.c
 * drives the em_* function directly.
 */

#ifndef REVOICE_ANDROID_LOCALHOST_H
#define REVOICE_ANDROID_LOCALHOST_H

#include <stddef.h>

struct addrinfo;

#ifdef __cplusplus
extern "C" {
#endif

/* Is this one of the names that must resolve to the loopback literal?
 * Exposed for the test. */
int em_is_loopback_name(const char *node);

int em_getaddrinfo(const char *node, const char *service,
                   const struct addrinfo *hints, struct addrinfo **res);

#ifdef __ANDROID__
#define getaddrinfo em_getaddrinfo
#endif

#ifdef __cplusplus
}
#endif

#endif /* REVOICE_ANDROID_LOCALHOST_H */
