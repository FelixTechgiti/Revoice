"""
Asking a device why a local endpoint is not working, in one shell round trip.

**Written because a whole evening of this could not be answered from here.**
On 2026-09-20 a device took emOS 0.6.0-fx.1 — the release whose entire content
is a DNS proxy for bionic — and librespot went on exiting once a minute with
`could not initialize spirc: Service unavailable { client error (Connect) }`,
the exact failure that release ends. Two explanations fit that equally well
and want opposite next steps: the image is not the one we think it is, or the
proxy is running and something else is wrong. Nothing the controller could
read separated them, and the person who could open a shell was asleep.

So each probe here is chosen to SEPARATE two explanations, never to confirm
one. The rules that came out of that:

- **Resolution is proven by the address, not by a reply.** `ping` printing
  `PING host (1.2.3.4)` has resolved the name even if every packet is lost;
  a device behind a router that drops ICMP would otherwise read as a device
  that cannot resolve, which is the fault we are hunting.
- **The probe must be a BIONIC program.** busybox on these devices is static
  and carries its own resolver reading `/etc/resolv.conf` — a file emOS
  writes and nothing else reads — so `busybox ping` answers for a resolver no
  endpoint uses. Amazon's `/system/bin/ping` is dynamically linked against
  `/system/lib/libc.so`, which is the resolver librespot and shairport-sync
  actually use. This is the one place in this tree where the Android binary
  is deliberately preferred to busybox, and it is preferred BECAUSE it is the
  one under test.
- **"Could not ask" is never an answer about the device.** Every field
  collapses to unknown rather than to a verdict when its probe did not run.
"""

# The sentinel that says the probe RAN, for `em_netflash.PROBE_MARK`'s reason:
# an empty answer must read as "no shell", never as a device with nothing
# listening and no resolver.
DIAG_MARK = "_DIAGCHK"

# What a working name lookup is asked for. Spotify's own resolver endpoint,
# because it is the name whose failure started this and because a device that
# can reach it can reach the service — a synthetic name would answer a
# question nobody has.
DNS_PROBE_HOST = "apresolve.spotify.com"

# **bionic has TWO resolver paths and they are not interchangeable.**
# `getaddrinfo` is what Rust, Go and anything modern uses — librespot and
# shairport-sync among them — and `gethostbyname` is the older call that
# Amazon's own `/system/bin/ping` takes. emOS answered only the first until
# 0.7.0-fx.1, so the obvious test of name resolution measured the one path
# that was never implemented and reported a broken resolver on a device whose
# resolver worked. Both are probed, separately, and the verdict prefers the
# one the endpoints use.
#
# The getaddrinfo probe talks to the socket directly rather than through a
# program, because there is no bionic binary on the device that is known to
# take that path — so the conversation is held in the words bionic would use.
# `busybox nc -U` is not on every build; where it is missing the probe is
# UNTESTABLE, which is a third answer and not a failure.
GAI_PROBE = "getaddrinfo"

# The two ports pinned in `device/internal/netfilter`, plus AirPlay 2's own.
# Mirrored rather than imported: this module stays dependency-free, and the
# numbers are pinned against the firmware by test.
PORT_AIRPLAY_RTSP = 5000
# shairport-sync's AirPlay 2 build defaults to 7000 and THIS firmware pins it
# to the port above for both flavours (`netfilter.AirPlayRTSPPort`, written
# into the config the receiver is started with). So 7000 listening is not
# expected here, and treating its absence as a fault would accuse every
# working AirPlay 2 device — it is reported and never judged.
PORT_AIRPLAY2_DEFAULT = 7000
PORT_SPOTIFY_ZEROCONF = 36000

AP2_BINARY = "/data/local/bin/shairport-sync-ap2"
CLASSIC_BINARY = "/data/local/bin/shairport-sync"
NQPTP_BINARY = "/data/local/bin/nqptp"

# emOS's own boot and network log (`NETLOG` in emos/init/init.c).
NETLOG = "/run/net.log"


def diag_cmd() -> str:
    """Everything worth asking a device whose endpoints are misbehaving.

    One command and one round trip, because the caller already pays ~26s for
    a shell session and a second question would double it for a tab somebody
    is watching.

    `2>&1` on the ping is deliberate: its failure line is the answer, and a
    probe that hides the one sentence naming the cause is the mistake
    `flash_cmd`'s discarded output already cost this project once.
    """
    return (
        f"[ -S /dev/socket/dnsproxyd ] && echo DNSSOCK:yes || echo DNSSOCK:no; "
        # The resolver conversation ITSELF, in the words bionic uses. `ping`
        # below takes the OTHER of bionic's two paths — see GAI_PROBE — so
        # this is the one that answers for librespot and shairport-sync.
        f"echo \"GAI:$(printf 'getaddrinfo {DNS_PROBE_HOST} ^ 0 0 0 0 0 0\\0' "
        # `busybox head`, not `head`: a bare name is toybox on FireOS 6 and has
        # no `-c` (#320). This one does not fail loudly — it would return a
        # wrong answer about DNS on exactly the platform #263 is open for.
        f"| busybox nc -U /dev/socket/dnsproxyd 2>&1 | busybox head -c 4)\"; "
        f"echo \"PING:$(/system/bin/ping -c 1 -w 2 {DNS_PROBE_HOST} 2>&1 "
        f"| busybox head -1)\"; "
        f"echo \"PORTS:$(busybox netstat -ltn 2>/dev/null "
        f"| busybox awk '{{print $4}}' | busybox tr '\\n' ' ')\"; "
        f"echo \"AP2:$([ -f {AP2_BINARY} ] && echo yes || echo no)\"; "
        f"echo \"CLASSIC:$([ -f {CLASSIC_BINARY} ] && echo yes || echo no)\"; "
        f"echo \"NQPTP:$([ -f {NQPTP_BINARY} ] && echo yes || echo no)\"; "
        f"echo \"NQPTPRUN:$(busybox ps 2>/dev/null | busybox grep -c "
        f"'[n]qptp')\"; "
        # emOS's init writes its own account of the boot and the network to
        # /run/net.log, and nothing in this controller has ever read it. It
        # is where `dnsproxyd: could not bind` would be — the one explanation
        # the probes above cannot produce, because a socket that was never
        # created looks exactly like an init too old to create one.
        f"echo \"NETLOG:$(busybox tail -n 6 {NETLOG} 2>/dev/null "
        f"| busybox tr '\\n' '|')\"; "
        f"echo {DIAG_MARK}")


def _gai_verdict(raw: str):
    """What the direct socket conversation said: "ok", "refused" or None.

    None means the probe could not run — no `nc -U`, no socket, an error
    message where a code was expected. That is not evidence about the
    resolver, and the caller falls back to the other path rather than
    reporting a failure nobody measured.
    """
    text = (raw or "").strip()
    if text.startswith("222"):
        return "ok"
    # Any other 3-digit code is the proxy refusing, which IS an answer about
    # the resolver: something is listening and it said no.
    if len(text) >= 3 and text[:3].isdigit():
        return "refused"
    return None


def _dns_verdict(ping_line: str, socket_present: bool):
    """What the ping's first line says about RESOLUTION, not about reachability.

    Five answers, and the two that look alike are the point: `no_socket` means
    emOS is not answering bionic at all (an init older than 0.6.0-fx.1, or one
    that could not bind), while `unresolved` means the socket is there and the
    lookup still failed — a proxy that is running and not working. Those want
    completely different next moves, and until this existed they presented
    identically as Spotify restarting once a minute.
    """
    line = (ping_line or "").strip()
    low = line.lower()
    if not line:
        return "unknown"
    # A missing probe binary says nothing about the device's resolver.
    if "not found" in low or "no such file" in low or "permission denied" in low:
        return "no_tool"
    # The resolved address rides the first line, in parentheses, whether or
    # not a single packet ever comes back.
    if "(" in line and ")" in line and "ping" in low:
        return "ok"
    if ("unknown host" in low or "bad address" in low
            or "name or service not known" in low
            or "temporary failure in name resolution" in low):
        return "unresolved" if socket_present else "no_socket"
    return "unknown"


def _ports(raw: str):
    """Listening TCP ports out of netstat's local-address column.

    Only the port is kept: the column is `0.0.0.0:5000`, `:::7000` or
    `127.0.0.1:9000` depending on the family, and which interface a receiver
    bound to is a different question from whether it is listening at all.
    """
    out = []
    for field in (raw or "").split():
        _, sep, port = field.rpartition(":")
        if not sep or not port.isdigit():
            continue
        n = int(port)
        if n not in out:
            out.append(n)
    return sorted(out)


def parse_diag(out: str):
    """Read `diag_cmd`'s answer, or None when the probe did not run.

    None is "could not ask", which every caller must render as a question
    nobody answered rather than as a device with no resolver and nothing
    listening.
    """
    text = out or ""
    if DIAG_MARK not in text:
        return None

    fields = {}
    for line in text.splitlines():
        line = line.strip()
        for key in ("DNSSOCK", "GAI", "PING", "PORTS", "AP2", "CLASSIC",
                    "NQPTP", "NQPTPRUN", "NETLOG"):
            if line.startswith(key + ":"):
                fields[key] = line[len(key) + 1:].strip()

    socket_present = fields.get("DNSSOCK") == "yes"
    ports = _ports(fields.get("PORTS", ""))
    running = fields.get("NQPTPRUN", "")
    gai = _gai_verdict(fields.get("GAI", ""))

    # The endpoints' own path decides when it could be measured; ping only
    # answers for Amazon's older tools, and on an emOS below 0.7.0-fx.1 it
    # fails whatever the resolver is doing.
    if gai == "ok":
        dns = "ok"
    elif gai == "refused":
        dns = "unresolved" if socket_present else "no_socket"
    else:
        dns = _dns_verdict(fields.get("PING", ""), socket_present)

    return {
        "dnsSocket": socket_present,
        "dns": dns,
        "dnsPath": ("getaddrinfo" if gai else "gethostbyname"),
        "dnsDetail": fields.get("PING", "").strip(),
        "ports": ports,
        "airplayListening": PORT_AIRPLAY_RTSP in ports,
        "airplay2Listening": PORT_AIRPLAY2_DEFAULT in ports,
        "spotifyListening": PORT_SPOTIFY_ZEROCONF in ports,
        "ap2Installed": fields.get("AP2") == "yes",
        "classicInstalled": fields.get("CLASSIC") == "yes",
        "nqptpInstalled": fields.get("NQPTP") == "yes",
        # A count, because `grep -c` answers 0 for "not running" and for "ps
        # did not run" alike — so a non-numeric answer is unknown, not zero.
        "nqptpRunning": (int(running) > 0) if running.isdigit() else None,
        # The init's own words, newest last, empty when the file is not there
        # (a FireOS device, or an init that never wrote one).
        "netlog": [part.strip() for part in
                   fields.get("NETLOG", "").split("|") if part.strip()],
    }


def with_intent(diag, *, airplay_on: bool, airplay2_on: bool,
                spotify_on: bool):
    """Fold in what the CONTROLLER asked for, beside what the device has.

    The device can only answer what is true of it; whether that is what
    anybody wanted is here. Both halves are needed to say anything useful —
    "no AirPlay 2 binary" is a fault when the setting is on and completely
    correct when it is off, and a panel that reports only the device half
    accuses a working device in front of the person who switched it off.
    """
    if diag is None:
        return None
    out = dict(diag)
    out["airplayWanted"] = bool(airplay_on)
    out["airplay2Wanted"] = bool(airplay2_on)
    out["spotifyWanted"] = bool(spotify_on)
    return out


def summary(diag):
    """One line for somebody who is not going to read the table.

    Ordered by what blocks what: nothing local works without resolution, so
    that is said first and the rest is not piled on top of it.

    Every verdict about an endpoint is gated on that endpoint being SWITCHED
    ON. A device serving classic AirPlay because nobody asked for AirPlay 2
    is not a fault, and reporting one teaches people to ignore this line.
    """
    if diag is None:
        return "not_asked"
    if diag["dns"] == "no_socket":
        return "dns_no_socket"
    if diag["dns"] == "unresolved":
        return "dns_unresolved"
    if diag["dns"] in ("unknown", "no_tool"):
        return "dns_unknown"
    wanted_airplay = diag.get("airplayWanted", True)
    wanted_spotify = diag.get("spotifyWanted", True)
    wanted_ap2 = diag.get("airplay2Wanted", False)

    # Only endpoints somebody asked for can be silent. A device with both
    # switched off is listening on nothing and is perfectly healthy.
    silent = ((wanted_airplay and not diag["airplayListening"])
              or (wanted_spotify and not diag["spotifyListening"]))
    if silent and (wanted_airplay or wanted_spotify):
        return "endpoints_silent"
    if wanted_ap2 and not diag["ap2Installed"]:
        return "ap2_not_installed"
    if wanted_ap2 and not diag["nqptpInstalled"]:
        return "ap2_without_clock"
    if wanted_ap2 and diag["nqptpRunning"] is False:
        return "ap2_clock_not_running"
    return "ok"
