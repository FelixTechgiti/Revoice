# igmpprobe — re-assert a multicast membership from the device, by hand

Sends the unsolicited IGMP membership report the kernel will not, so an
access point that has aged its snooping entry out starts forwarding the group
again. Nothing is restarted and no socket is touched.

```
$ /data/local/tmp/igmpprobe
v2 report -> group: 32 bytes
v3 report -> 224.0.0.22: 40 bytes
```

## Why it exists

Linux emits an unsolicited membership report only when a group's refcount on
an interface goes 0 to 1. Revoice holds 224.0.0.251 continuously with two
long-lived responder sockets — librespot's `libmdns` and shairport-sync's
`tinysvcmdns` — so after boot the membership is asserted once and never
renewed. Where the access point ages its entry out, inbound multicast stops;
and because the IGMP General Query that would prompt a fresh report is itself
multicast, the device can never hear the one message that would end it. The
Echo then answers no query, and Spotify Connect and AirPlay are invisible
until something re-joins the group.

The firmware now prevents that on a cadence (`internal/mcast/report.go`).
This is the instrument that proved the mechanism, and the lever for a device
running firmware that predates it.

## What it measured, 2026-10-12, on Studio

Both endpoints alive for 9.8 days, the membership present in `/proc/net/igmp`
throughout, and `Udp: InDatagrams` from `/proc/net/snmp` frozen across a
20-second browse that six other Spotify hosts answered:

```
852294..852299  frozen at 1414559      six seconds, nothing arriving
SEND            v2 -> group, v3 -> 224.0.0.22
852309          1414565                +6, nine seconds later
852313          1414568                +3
```

The controller's scan then read AirPlay as visible with its RTSP port
reachable. **Restarting one endpoint does not do this**: that takes the
refcount 1 to 2, emits no report, and leaves the device exactly as invisible.

## Usage

```
igmpprobe [group] [count] [interval-seconds] [interface]
```

Defaults: `224.0.0.251`, once, no interval, `wlan0`. With a count and an
interval it keeps reporting, which is how to hold a device visible while
something else is being measured.

It needs root for the raw socket, which every shell on these devices has.

## Build and deploy

`./build.sh` — freestanding, no libc, ~2KB, for `mdnsprobe`'s reasons: it
reaches a device as base64 through the controller's shell plane, and
depending on no libc removes the bionic question rather than answering it.

```bash
CC=arm-linux-gnueabihf-gcc STRIP=arm-linux-gnueabihf-strip ./build.sh
gzip -n -9 -c igmpprobe | base64 -w0
```

then on the device, through the dashboard shell or `POST /api/devices/{id}/exec`:

```sh
echo -n '<base64>' > /data/local/tmp/p.b64
busybox base64 -d /data/local/tmp/p.b64 | busybox gunzip > /data/local/tmp/igmpprobe
chmod 755 /data/local/tmp/igmpprobe
md5sum /data/local/tmp/igmpprobe
```

**Compare the md5 against the local binary before running it** — a single
mistyped base64 character produces a file of exactly the right length. Note
`-n` on gzip: without it the header carries an mtime, so two runs of one
source differ and a digest taken later reports corruption that is not there.

## How to tell whether it worked

The counter, not the feature. `grep '^Udp:' /proc/net/snmp` once a second:
the second field is `InDatagrams`, and on a device whose group is being
forwarded it moves within seconds of any mDNS on the link. Reading the
Spotify or AirPlay picker instead measures a cache as much as a device.
