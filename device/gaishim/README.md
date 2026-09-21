# gaishim — getaddrinfo for the endpoints on emOS

`librespot`, `shairport-sync` and `nqptp` all resolve a name before they do
anything useful. On emOS none of them can, and that is the whole of why
Spotify Connect and AirPlay are dead there (#263).

This is a ~5KB shared library that answers `getaddrinfo` itself. The firmware
preloads it into those three processes when the device is running emOS and the
file is installed; on FireOS it is never loaded.

## What is actually broken

bionic's `getaddrinfo` and its `gethostbyname` both go to
`/dev/socket/dnsproxyd`, both are answered by the same `dnsproxy_serve` in
`emos/init/init.c`, and only `gethostbyname` works.

The proxy is not at fault. Measured on 2026-09-21 with `device/tools/dnsprobe`
(#264),
on G090L91180250AN1 under emOS 0.7.0-fx.1:

- the proxy's reply is netd's serialisation **byte for byte**, with the right
  address;
- Amazon's bionic rejects it, and rejects **every other shape netd could
  send** — eight were tried;
- with the connection held open rather than closed, bionic **blocks**, so it
  wants more per entry than AOSP android-5.1.1_r38 does.

Matching netd harder cannot succeed against a parser that wants something netd
never writes. Reading `android_getaddrinfo_proxy` out of the device's own
`/system/lib/libc.so` would settle the format; it is a disassembly job, it is
still open as #263, and it is not needed for the endpoints to work.

## Why a preload rather than a fix in emOS

emOS ships only inside a boot image the user assembles from their own boot
partition. A fix there cannot reach a device that is already running. This
travels the road librespot and shairport-sync already travel — an endpoint
binary, installed from the dashboard or fetched from an `endpoints-v*` release.

It also fixes #219 for these three programs, without an emOS release: `localhost`
is answered inside the shim, before any lookup, so nqptp's control port resolves
on a network whose router answers NXDOMAIN for that name.

## What it is not

Not a general resolver. There is no AAAA lookup, because the device's
`gethostbyname` answers A records and nothing else — an explicit `AF_INET6`
**name** lookup is refused with `EAI_ADDRFAMILY` rather than answered with an A
record in a v6 sockaddr. IPv6 **literals** and the passive wildcard are answered
in full, because they need no lookup and a receiver that binds `::` must still
be able to.

## Building

```bash
./build.sh          # needs an NDK: ANDROID_NDK_HOME, ANDROID_NDK_ROOT or NDK
```

Three things run before the compile and any of them fails the build:

| check | what it catches |
|---|---|
| `gaishimcheck.c` | drives the real `getaddrinfo` on the build host — layout, port byte order, every refusal, and leaks |
| `abicheck.c` | every constant `gaishim.c` hardcodes, against Android's own headers for the real target |
| the export check | a library that loads and interposes **nothing**, which is otherwise completely silent |

The third is not hypothetical: the first build of this library exported zero
symbols, loaded cleanly and changed nothing.

CI runs all of it in the pinned compiler image, against the same NDK that
builds the firmware — `abicheck.c` is only meaningful against the headers the
callers were compiled with.

## Verification status

**In CI verifiziert.** The checks above and the ARM ELF assertions.

**Not verified on hardware.** No device has yet run an endpoint with this
preloaded. What that would answer, and nothing else can: whether librespot
gets past `clienttoken.spotify.com`, and whether shairport-sync and nqptp
reach each other over `localhost`.
