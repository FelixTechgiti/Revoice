# mcastnudge — change the interface's multicast address list, and see if that is what the restart does

Joins an unrelated multicast group on wlan0, holds it briefly, and leaves
again. Nothing else. The point is the side effect: both the join and the
leave change the interface's address list, which is what makes the kernel
call the driver's `set_rx_mode` and re-program the chip's filter.

```
$ /data/local/tmp/mcastnudge
join: ok
leave: ok
address list changed twice per cycle; read the counters, not this line
```

## Why it exists, and what it disproved

Restarting both responders is the one repair that reliably brings inbound
multicast back on this board, and it costs whatever is playing. The
explanation written down for it (`internal/mcast/allmulti.go`) was that the
repair works by SIDE EFFECT: 224.0.0.251's refcount on wlan0 goes 0 then 1,
the address list changes, and the re-program is what fixes it. If that were
true, changing the list in a way nobody is using would fix it just as well,
for free.

**It does not.** Measured on Studio, 2026-10-13 04:23, on a device that had
been deaf for 60 seconds inside a ten-minute episode:

```
04:23:41  mcast=45717   join+leave of 239.255.77.77
04:23:42..04:23:58  mcast=45717 for sixteen more seconds
```

The firewall's counter on the `-d 224.0.0.251 dpt:5353` rule did not move at
all. So re-programming the filter is not what the restart does, and the
explanation in `allmulti.go` needs the correction this result provides.

**The instrument was verified before the result was believed**, which is the
half worth copying. A join that silently did nothing would have produced the
same output. During the hold:

```
/proc/net/igmp      4D4DFFEF  1        <- 239.255.77.77, little-endian
/proc/net/dev_mcast 01005e7f4d4d       <- its derived MAC, newly present
                    (7 entries before, 8 during)
```

So the list really changed and the driver really was called.

## What is left

The restart works for some other reason. The remaining candidate is the IGMP
membership report the kernel emits on a genuine 0 -> 1 transition **of
224.0.0.251 itself**, reaching the access point and un-aging its snooping
entry — which `device/tools/igmpprobe` could not reproduce by hand, and the
difference between a hand-built report and the kernel's own has never been
captured. That comparison is the next measurement, not another repair.

## Usage

```
mcastnudge [group] [ifindex] [cycles] [hold-ms]
```

Defaults: `239.255.77.77`, ifindex `7`, one cycle, 500ms. The index is
wlan0's, as the first column of its `/proc/net/igmp` section prints it —
confirm it rather than assuming, since it is not stable across boots in
general.

224.0.0.251 is **refused**: the interface already holds it, so joining it
again changes no address list and re-programs nothing. Allowing it would
have measured nothing while looking like it had.

The default group is administratively scoped (RFC 2365) and assigned to no
protocol, so holding it for half a second asks nothing of anybody else's
equipment and receives nothing.

It needs no special privilege beyond opening a socket.

## Build and deploy

`./build.sh` — freestanding, no libc, ~1.8KB, for `mdnsprobe`'s reasons: it
reaches a device as base64 through the controller's shell plane, and
depending on no libc removes the bionic question rather than answering it.

```bash
CC=arm-linux-gnueabihf-gcc STRIP=arm-linux-gnueabihf-strip ./build.sh
gzip -n -9 -c mcastnudge | base64 -w0
```

then on the device, through the dashboard shell or `POST /api/devices/{id}/exec`:

```sh
echo -n '<base64>' > /data/local/tmp/n.b64
busybox base64 -d /data/local/tmp/n.b64 | busybox gunzip > /data/local/tmp/mcastnudge
chmod 755 /data/local/tmp/mcastnudge
md5sum /data/local/tmp/mcastnudge
```

**Compare the md5 against the local binary before running it** — a single
mistyped base64 character produces a file of exactly the right length. Note
`-n` on gzip: without it the header carries an mtime, so two runs of one
source differ and a digest taken later reports corruption that is not there.

## How to tell whether it worked

The counter, not the feature, and the right counter. The firewall's own
packet count on the `-d 224.0.0.251 dpt:5353` rule
(`iptables -L INPUT -n -v`) counts only multicast mDNS and cannot be cached;
`Udp: InDatagrams` from `/proc/net/snmp` also moves for unicast, including
the shell session doing the measuring, so a reading of "+2" there means
nothing.
