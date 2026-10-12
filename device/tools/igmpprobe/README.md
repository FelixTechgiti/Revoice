# igmpprobe — re-assert a multicast membership from the device, by hand

Sends the unsolicited IGMP membership report the kernel will not, so an
access point that has aged its snooping entry out starts forwarding the group
again. Nothing is restarted and no socket is touched.

```
$ /data/local/tmp/igmpprobe
v2 report -> group: 32 bytes
v3 report -> 224.0.0.22: 40 bytes
```

## Why it exists, and what it disproved

It was built to test one theory: that the access point had aged its IGMP
snooping entry out, so the group was no longer being forwarded, and that an
unsolicited membership report would restore it. Linux emits such a report only
when a group's refcount on an interface goes 0 to 1, and librespot and
shairport-sync hold 224.0.0.251 for the life of the process — so nothing ever
renews it.

**The theory is wrong on this hardware, and this tool is what settled it.** A
report pair sent by hand moved `Udp: InDatagrams` by **zero over sixteen
seconds**, on a device that had been deaf for eight minutes. The fault is the
Echo's own multicast address filter: `ip link set wlan0 allmulticast on`
restores reception in two seconds with nothing else touched, which is what the
firmware now does (`internal/mcast/allmulti.go`).

It is kept because that is a result worth being able to reproduce, and because
sending a membership report by hand is otherwise not possible on a device with
no `ip maddr` write and no mDNS tooling.

**One earlier run appeared to support the theory** — traffic resumed nine
seconds after a report — and it did not replicate. On an intermittent signal,
one observation of something starting again says nothing about what started
it. A whole firmware release was cut on that observation before it was re-run.

## What a deaf device looks like, for comparison

```
857856..857859  frozen at 1449228, firewall rule for 224.0.0.251 at 1 packet
                ip link set wlan0 allmulticast on
857862          1449231, rule 2
857868          1449237, rule 4
857873          1449240, rule 5
```

Measured on Studio, 2026-10-12. `/proc/net/igmp` held the group and
`/proc/net/dev_mcast` carried `01005e0000fb` throughout, which is why nothing
on the device reported the difference.

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
