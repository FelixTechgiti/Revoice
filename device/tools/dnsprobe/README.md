# dnsprobe — measuring emOS's DNS proxy against the client that has to read it

Under emOS nothing resolves names for bionic except the proxy in
`emos/init/init.c`, and when it goes wrong the only symptom anywhere is
`EAI_NODATA`: no log at either end, and a program that says a name did not
resolve. That is indistinguishable from a dead network, a wrong nameserver and
a missing socket. `emos/init/dnscheck.c` covers the proxy's own functions
off-target; these three tools cover everything between them and a real program
on a real device.

Build with `./build.sh`; push with the recipe it prints.

| tool | what it answers |
|---|---|
| `dnsprobe` | what the proxy REPLIES — connects to `/dev/socket/dnsproxyd`, sends a request line and hexdumps the answer |
| `gaiprobe.so` | what BIONIC makes of that reply — LD_PRELOADed into one of the device's own 32-bit binaries, so libc is fully initialised |
| `fakeproxy` | what bionic WOULD accept — stands in for the proxy and serves a reply you choose |

## dnsprobe

```
dnsprobe [host]                 # one getaddrinfo and one gethostbyname
dnsprobe -r '<request line>'    # send exactly this line
```

The `-r` form exists because the request is the variable under test: bionic's
own format strings are readable on the device
(`busybox strings /system/lib/libc.so | grep 'getaddrinfo %'`), and a shape it
produces has to be reproducible without a rebuild.

## gaiprobe.so

```
LD_PRELOAD=/data/local/tmp/gaiprobe.so /data/local/bin/nqptp
```

A constructor runs the calls and `_exit`s, so the host process never reaches
its own `main`. **The host must be 32-bit** — `/system/bin/ping` is aarch64 on
biscuit, and a 32-bit `.so` will not load into it. It also measures bionic's
own `sizeof(struct addrinfo)`, by resolving a literal address (which never
reaches the proxy) and taking the distance from the returned struct to the
sockaddr bionic placed immediately behind it. That number is what the proxy's
blob length is compared against, and it cannot be assumed from the build.

## fakeproxy

```
fakeproxy <mode> <connections>
```

It binds `/dev/socket/dnsproxyd`, so the real one is moved aside first and put
back afterwards:

```sh
S=/dev/socket/dnsproxyd
mv $S $S.real; ./fakeproxy 0 4 >/dev/null 2>&1
LD_PRELOAD=/data/local/tmp/gaiprobe.so /data/local/bin/nqptp
rm -f $S; mv $S.real $S
```

**The rename is safe and the restore is exact**: a unix socket's listener is
bound to the inode, not to the name, so init's proxy neither notices nor needs
restarting. Two things that cost a measurement each:

- **Restore in a separate command from the one that runs the probe.** A mode
  that makes bionic block takes the shell's timeout with it, and then the
  restore never runs and the device has no resolver until someone notices.
- **Never `pkill -f fakeproxy`.** The shell running the script has that string
  in its own command line, so it kills itself, and the run looks like a hang
  in the probe.

Modes 0–7 vary the reply's shape (length, canonical name, terminator, pointer
fields, sockaddr size, entry count); 8 and 9 hold the connection open instead
of closing it, which separates "bionic read this and rejected it" from "bionic
is still waiting for more".
