/*
 * android_localhost.c — getaddrinfo("localhost") on a device that has no
 * resolver answering for it.
 *
 * AirPlay 2 is two processes talking over a UDP control port, and both of them
 * find that port by NAME: nqptp binds it with
 * `open_sockets_at_port("localhost", NQPTP_CONTROL_PORT, ...)` and
 * shairport-sync sends to it from `ptp_send_control_message_string`. Neither
 * name is configurable. Both die if the lookup fails — nqptp in
 * nqptp-utilities.c, under a `die("getifaddrs: ...")` whose label is upstream's
 * mistake and whose call is getaddrinfo, which is why the two failures read as
 * different faults in a log and are one.
 *
 * On this hardware the lookup DOES fail. bionic resolves a name through
 * /dev/socket/dnsproxyd, and under emOS the thing on the other end is our own
 * proxy in emos/init/init.c, which sends every non-literal name to the upstream
 * nameserver; a router answering NXDOMAIN for `localhost` is then an EAI_NODATA
 * at the caller. Measured on the fleet 2026-09-19: both binaries exiting every
 * minute for hours, so AirPlay 2 had never once started. netd has a hosts-file
 * path and does not do this, which is why it does not happen under FireOS — and
 * which is why fixing the proxy (#219) is the general repair and this is not.
 *
 * This is the half that can reach a fielded device, because a shim rides the
 * endpoint binaries and the proxy rides a boot image somebody has to flash. It
 * is also base-independent: rewriting the name costs nothing where the name
 * already worked.
 *
 * Two properties are load-bearing.
 *
 * **The node is REWRITTEN, never fabricated.** Handing back an addrinfo of our
 * own would put our allocation in front of the real freeaddrinfo, which both
 * callers use. Passing the literal instead keeps every struct upstream's: a
 * numeric node is resolved by bionic locally, without the proxy, and emOS's
 * proxy checks inet_pton before it queries anyway, so the literal never leaves
 * the device under either base.
 *
 * **IPv4 only, deliberately.** `ptp_send_control_message_string` uses the FIRST
 * result and nothing else — one socket, one sendto — while nqptp binds every
 * result it is given and needs only one to succeed. So a `::1` in front of a
 * `127.0.0.1` is a control message sent to an address nqptp may never have
 * bound, with nothing failing at either end and a clock that simply never
 * ticks. One family cannot disagree with itself.
 */
/* The rename is undone for THIS file, and it is not enough to merely leave our
 * own header out — the shim is injected with -include, so the macro is in scope
 * in every translation unit of the build including this one, and the call at
 * the bottom would be a call to itself. Recursion until the stack runs out,
 * from a file whose whole job is to reach the real function.
 *
 * It has to come BEFORE <netdb.h>, which is the part that is easy to get wrong
 * and compiles either way. With the macro still in scope, the preprocessor
 * rewrites netdb.h's own prototype into a declaration of em_getaddrinfo — so
 * undoing the rename afterwards leaves the real function with no prototype at
 * all, and the call below becomes an implicit declaration returning int.
 * Measured on the host, 2026-09-19: it links and it works, which is exactly why
 * nothing would have caught it. */
#undef getaddrinfo

#include <netdb.h>
#include <stddef.h>
#include <strings.h>

/* Declared rather than included, the android_ifaddrs.c trick: our own header
 * would put the macro straight back. */
int em_is_loopback_name(const char *node);
int em_getaddrinfo(const char *node, const char *service,
                   const struct addrinfo *hints, struct addrinfo **res);

/* The names a hosts file maps to 127.0.0.1, and no others.
 *
 * Narrow on purpose. `ip6-localhost` and `localhost6` mean the IPv6 loopback,
 * and answering them with an IPv4 address would be a wrong answer rather than a
 * missing one — the failure this file exists to end is a lookup that returns
 * nothing, not one that returns something unexpected. Nothing in either program
 * asks for them. */
int em_is_loopback_name(const char *node) {
  if (node == NULL)
    return 0;
  return strcasecmp(node, "localhost") == 0 ||
         strcasecmp(node, "localhost.localdomain") == 0;
}

int em_getaddrinfo(const char *node, const char *service,
                   const struct addrinfo *hints, struct addrinfo **res) {
  if (em_is_loopback_name(node))
    node = "127.0.0.1";
  return getaddrinfo(node, service, hints, res);
}
