# emOS changelog

## 0.6.0-fx.1

### Namen lassen sich jetzt auflösen

**Spotify Connect hat auf emOS nicht funktioniert, und der Grund war, dass es
gar keine Namensauflösung gab.** librespot konnte `apresolve.spotify.com` nicht
nachschlagen, ist beendet worden und endlos neu gestartet. Am Gerät gemessen
(2026-09-15): Netz und Routing in Ordnung, Ping und TCP gehen — aber eine
DNS-Anfrage verlässt das Gerät nicht einmal.

Amazons bionic löst Namen nicht selbst auf. Sie reicht sie über einen
Unix-Socket an Androids `netd` weiter, und den gibt es hier nicht. In der libc
des Geräts kommt `/dev/socket/dnsproxyd` vor, die Zeichenkette `resolv.conf`
dagegen **null Mal** — die Datei, die emOS schreibt, hat nie jemand gelesen.

emOS' Init beantwortet diesen Socket jetzt selbst: es nimmt die Anfrage an,
fragt den Nameserver aus `/etc/resolv.conf` und gibt die Adressen im Format
zurück, das bionic erwartet. Damit hat **jedes gegen bionic gelinkte Programm**
auf dem Gerät wieder DNS, nicht nur librespot.

### Was du merkst

- **Spotify Connect läuft.** Der Echo taucht in der Spotify-App auf und spielt.
- **Zu tun ist nichts**, ausser dieses emOS-Update einzuspielen.
- IPv6 wird bewusst nicht beantwortet: Dieses Gerät hat nur eine link-lokale
  IPv6-Adresse, und eine AAAA-Antwort wäre ein Ziel, das der Aufrufer nicht
  erreicht — das scheitert dann erst beim Verbinden statt beim Auflösen, was
  schlechter zu finden ist.

Release notes for the emOS init, one section per version. The heading is
the version WITHOUT the `emos-v` prefix, because that is what
`.github/workflows/cut-release.yml` matches when it builds the tag
annotation from this file — `## 0.4.0-fx.1` for the tag `emos-v0.4.0-fx.1`.

Headings INSIDE an entry are `###` or deeper. A `## ` line starts a new
version section, and that is what the extractor stops at.

**Why three components where upstream uses two.** Upstream tags `emos-v0.4`.
This fork carries an `-fx.N` suffix on every release (see the root
CLAUDE.md), and every guard that reads a version here — the shape check in
`cut-release.yml`, `tests/test_changelog_headings.py` — is written for
`MAJOR.MINOR.PATCH`. Padding the patch digit keeps all of them exactly as
they are, and `0.4.0-fx.1` is an honest name for this fork's first build of
upstream's 0.4. Nothing parses the emOS version numerically: `build.sh`
stamps `git describe` into `/etc/os-release` and
`em_api._fetch_latest_emos_release` selects on the `emos-v` prefix and an
`init` asset.

Newest first. Written for the person deciding whether to write this to the
boot partition of a device they rely on.

## 0.5.0-fx.1

**The first emOS release of this fork that publishes everything an image
needs.** 0.4.0-fx.1 attached one asset, the aarch64 `init`, which is enough
for a FireOS 5 device and nothing else. This release adds the payload
bundle — `init`, `init32`, `wpa_supplicant`, `wpa_cli` and `em-wifi`, with a
manifest of sha256s — so the provisioning wizard can build an image for
FireOS 6 as well, and so a FireOS 5 device stops fetching an init that predates
850-odd lines of changes to `init.c`.

Upstream's 0.5 line plus what the 2026-09-14 sync carried past its tag. The
fork's own changes remain the rename and the paths that moved with it.

### emOS boots on FireOS 6

amonet-biscuit v2.0.0 boots only FireOS 6, so until now a device unlocked with
it had no emOS at all — the wizard refused it at the connect step, correctly,
because the aarch64 init cannot run under a 32-bit kernel.

FireOS 6 is the same Linux 3.18.19 compiled as 32-bit ARM, so the port is
paths and an architecture rather than drivers:

- `build.sh` reads the reference kernel's architecture and builds a matching
  init. An init of the wrong architecture boots to **nothing at all**, with no
  output, which is indistinguishable from a kernel that never started — so it
  is detected rather than configured.
- FireOS 6 is system-as-root: the real tree sits in a nested `system/`, so
  every absolute `/system/...` path is one directory short there. The nested
  tree is bind-mounted over the mountpoint, which leaves every path in `init.c`
  alone. A prefix cannot work — `vendor` and `etc` inside that partition are
  absolute symlinks that would then point at themselves.
- `wmt_loader`, the combo-chip launcher and `busybox` are resolved by what is
  present, and every caller falls back to the FireOS 5 path. **A device on
  FireOS 5 therefore resolves to exactly the paths it used before.**

### emOS brings its own WiFi userspace

Amazon's `wpa_supplicant` aborts under emOS before `main()` — it opens
`/dev/binder` — so a FireOS 6 image needs one of ours or it has no network and
no way to say so but a cable. `wpa_supplicant` and `wpa_cli` are built from
pinned sources (hostap by sha256, libnl-tiny by commit) in the same
digest-pinned toolchain as the init, and ship static ARM32.

**They are installed only into a FireOS 6 image**, and that restraint is
deliberate: init prefers `/sbin/wpa_supplicant` the moment one exists, so
including them in a FireOS 5 image would move the whole fleet off Amazon's
working supplicant as a side effect of an unrelated change.

`em-wifi` rides with them — a console tool that scans, joins and then **waits
to see the device actually associate**, so a device that is on emOS but not on
the network can be put on it over the serial cable alone.

### One bundle rather than five assets

Everything above travels as `emos-payload.zip` with a manifest of sha256s,
because five assets fetched in sequence can each fail on their own and hand a
build a mismatched set of parts. The format lives in
`controller/em_emos_build.py` and the release reads its own output back through
that same reader, so a release cannot publish a bundle its own consumer would
refuse.

The loose `init` is still published beside it, and that is compatibility rather
than duplication: a controller already in the field selects an emOS release by
matching that exact asset name.

### The device numbers now come from the kernel

`/dev/input/*` and `/dev/snd/*` are resolved through
`/sys/class/<cls>/<name>/dev` and fall back to the compiled-in table only where
sysfs is silent. Sysfs wins when the two disagree, because it describes the
running kernel while the table describes the board it was read off.

The `stage=mounts` line of the boot trail now says which happened —
`nodes=<from sysfs>/<total>`, with `differ=` counting the rows where kernel and
table disagreed. On biscuit `differ=0` is the expected reading.

The **block** nodes are still numbered by hand, and for those init **reports
rather than acts**: the same line carries the partition labels read off the GPT
(`gpt=`, `p10=`, `p13=`, `p15=`, `p16=`). Nothing acts on them yet. p16 is
`/data`, the one partition whose loss a remote user cannot undo, and choosing it
with code nobody has watched run on hardware is not a trade worth making for
tidiness.

### Smaller things

- The console's login records survive the project rename in both directions,
  so a device that crosses a rename boundary is not locked out of its own
  console.
- Three more off-target checks — `nodecheck`, `pathcheck`, `wpacheck` — each
  driving a parser whose failure is silent on hardware. They `#include init.c`
  whole and drive the real functions rather than reimplementing them, and they
  run in CI and again against the exact source being published here.

### What it costs, stated plainly

**FireOS 6 support has not run on hardware through the wizard.** The boot
itself was measured on a spare on 2026-09-12 under amonet v2.0.0, and the image
builder and the release assets are what was missing; that path is now complete
but unproven end to end. FireOS 5 is where the field devices are and is
unchanged in its paths.

**Status 0.5: bench-proven, not field-proven.** The known gaps are in
`emos/README.md`. `/data` survives a boot-partition write, so a device crossing
from FireOS keeps its Revoice install, its link credentials, its remembered
controller and its WiFi configuration. **Keep the boot image the wizard hands
you**: it is the undo, and restoring it takes about ten seconds.

> **⚠️ Do not install amonet-biscuit v2.0.0 on an Echo that is running
> today.** v2.0.0 replaces the Echo's bootloaders and FireOS 5 no longer boots
> after it, so a working device stops working. Unlock a new Echo with v1.1.0.
> A device that is **already** on v2.0.0 runs FireOS 6, and that is what this
> release's `init32` is for — it is no longer a dead end. Either way, **do not
> try to go back** by flashing FireOS 5 or an older amonet: that rewrites
> bootloaders by hand, which is how an Echo gets hard-bricked.

## 0.4.0-fx.1

**This fork's first published emOS init.** The code is upstream's 0.4 with
this fork's rename applied and nothing else — `emos/init/init.c` differs from
upstream only in the strings that say Revoice instead of EchoMuse and in the
paths that moved with the rename (`/data/local/etc/revoice/console.pw`, the
`svc_add("revoice", …)` service entry, the console banner, the USB
`iManufacturer`).

### Why this release exists at all

Until now **nobody using this fork could install emOS**, and nothing said so.
The provisioning wizard fetches the init from the repository named in
`github_repo`, which on this fork is `FelixTechgiti/Revoice` — and there was
no `emos-v*` tag on it, so `/api/provision/emos_init` answered 404 and the
wizard's emOS flow stopped at its build step. The emOS code has been in the
tree the whole time; only the published artifact was missing.

### What you get

emOS replaces Amazon's Android userspace entirely, keeping only the device's
own kernel. Against FireOS on the same hardware:

- **No `mediaserver`**, so nothing takes the speaker away from Revoice after
  an update and nothing rewrites the mixer behind us. That is the whole of
  the jack-detect and PCM-blocked class of faults.
- **No default-deny firewall.** Spotify Connect and AirPlay are reachable
  without the iptables rules firmware 2.31.0-fx.1 has to write under FireOS.
- **A USB serial console** on `/dev/ttyGS0` with a boot progress bar on the
  LED ring, so a device that will not come up can be asked why over a cable
  rather than guessed at.
- **`/init recovery`**, which reboots into TWRP from that console. Amazon's
  own `reboot` cannot: it talks to a property service emOS does not run and
  fails with `No such file or directory`, naming a socket rather than the
  real reason.
- **Rollback**: a boot is confirmed when the network comes up, and three
  unconfirmed boots restore the known-good image and show an amber ring.

### What it costs, stated plainly

**Status 0.4: bench-proven, not field-proven** — a small number of devices
over a handful of days. The known gaps are listed in `emos/README.md` and
none of them is a research problem, but read them before you flash a device
you depend on.

`/data` survives a boot-partition write, so a device that crosses from FireOS
keeps its Revoice install, its link credentials, its remembered controller
and its WiFi configuration. **Keep the boot image the wizard hands you**: it
is the undo, and restoring it takes about ten seconds.

> **⚠️ emOS needs the FireOS 5 kernel, so do not install amonet-biscuit
> v2.0.0.** v2.0.0 (10 September 2026) replaces the Echo's bootloaders, and
> after that FireOS 5 — and with it emOS — no longer boots. Unlock with
> **v1.1.0**. If you have already installed v2.0.0, **do not try to go back
> by flashing FireOS 5 or an older amonet**: that rewrites bootloaders by
> hand, which is how an Echo gets hard-bricked.
