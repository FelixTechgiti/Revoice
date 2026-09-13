# Third-party components

Revoice is MIT licensed (see `LICENSE`). It vendors and links the components
below, each of which keeps its own licence. All are compatible with
redistribution under MIT, and each requires that its copyright notice travels
with the software — which is what this file is for. All but one are also
permissive; the exception is libconfig, inside the published shairport-sync
binary, and it has a condition beyond attribution — see that section.

This fork publishes **four** kinds of artifact, and each carries its own
obligations:

- The **device firmware** (`v*` releases) is a combined work: it links SpeexDSP
  and GoTinyAlsa. BSD-3-Clause asks that binary redistributions reproduce the
  copyright notice "in the documentation or other materials provided with the
  distribution", and this file is that material.
- The **controller image** carries the pretrained models and Python
  dependencies listed further down.
- The **streaming endpoint binaries** (`endpoints-v*` releases) are other
  people's whole programs — see the section below, which is the one that was
  missing while those releases already existed.
- The **emOS init** (`emos-v*` releases) is this repository's own C, MIT. Why
  only the init is published, and never a boot image, is a licensing decision
  in its own right — `emos/README.md`, "Licensing", has it.

The three that run ON the device — firmware, endpoints, init — are built with
the Android NDK and **statically link bionic**, which is Apache-2.0 with
BSD-licensed imports. It is named here because a static link makes it part of
the artifact; the NDK's own terms permit redistributing what it compiles.

If any of them is ever distributed somewhere other than alongside this
repository, this notice needs to travel with it.

## Vendored into this repository

| Component | Where | Licence | Copyright |
|---|---|---|---|
| SpeexDSP (acoustic echo canceller) | `device/internal/aec/` | BSD-3-Clause | Xiph.Org Foundation, Jean-Marc Valin, Analog Devices, CSIRO |
| ONNX Runtime C API header | `device/internal/wakeword/ort/include/` | MIT | Microsoft |
| aioesphomeapi protocol buffers | `controller/esphome/vendor/` | MIT | Otto Winter |
| Home Assistant Voice PE timer sound (`timer_finished.flac`) | `controller/sounds/` | CC BY 4.0 | Clayton Charles Tapp |

Full licence texts sit beside the code, in `COPYING` or `*_LICENSE` files. Do
not remove them — they are the attribution the licences require. The timer
sound carries its attribution in `controller/sounds/LICENSE.md`, which ships
in the controller image alongside the audio: CC BY 4.0 asks that the credit
travel with the work, and the container is where the work actually goes.

## Linked as a submodule

| Component | Where | Licence | Copyright |
|---|---|---|---|
| GoTinyAlsa (`wilbowes/GoTinyAlsa` fork) | `GoTinyAlsa/` | BSD-3-Clause | binozoworks |

The fork exists to carry a `GetAudioStream` defer-in-loop leak fix; see
`device/CLAUDE.md` before repointing it upstream.

## Published as release assets: the streaming endpoints

`endpoints-v*` releases carry two upstream programs the controller installs
onto a device — **librespot** (Spotify Connect) and **shairport-sync**
(AirPlay). Neither project publishes an Android build, so this repository
cross-compiles them; the recipes are `device/librespot/` and
`device/shairport/`, and each names the exact upstream tag it builds.

Publishing somebody else's program means their notice travels with it, and
for a static binary that means every library inside it too. The
shairport-sync build links four:

| Component | Version | Licence | Copyright |
|---|---|---|---|
| librespot | v0.7.1 (recipe default) | MIT | Paul Lietar and contributors |
| shairport-sync | 4.3.7 (recipe default) | MIT | James Laird 2013, Mike Brady 2014–2026 |
| Mbed TLS | v2.28.8 | Apache-2.0 OR GPL-2.0-or-later (taken as Apache-2.0) | Arm Limited |
| libconfig | v1.8.2 | **LGPL-2.1-or-later** | Mark A. Lindner |
| popt | 1.19 | MIT | Red Hat, Inc. |

**libconfig is the one with a condition that is not just attribution.** It is
LGPL and it is linked STATICALLY, which is the case LGPL-2.1 §6 is written
for: a user has to be able to relink the program against a modified libconfig.
What discharges that here is that the whole build is reproducible from this
repository — `device/shairport/build.sh` pins libconfig to `v1.8.2` by tag and
builds it from source, so anyone can substitute their own and rebuild. Do not
switch libconfig to a vendored copy or an unpinned fetch without dealing with
this: the pin is what makes the offer meaningful.

The two Android compat shims (`device/librespot/ifaddrs_alias.c` and
`device/shairport/compat/`) are this repository's own code under the MIT
licence at `LICENSE`, and are compiled into the published binaries. MIT is
compatible with every licence above, in that direction.

## Installed at build or run time

These are dependencies rather than vendored code — they are fetched by the
Dockerfiles and `requirements.txt`, and are not redistributed as source here.
Listed because they end up in the published container image:

- **ONNX Runtime** (MIT) — wake word inference, controller and device.
- **openWakeWord** (Apache-2.0) — wake word models and feature pipeline.
- **DTLN** (MIT, Nils L. Westhausen) — the two pretrained noise-suppression
  models the controller image downloads at build time, pinned to a commit in
  `controller/Dockerfile`. Baked into the published image, so they are
  redistributed with it.
- **ffmpeg** (LGPL-2.1+ as packaged by Debian) — invoked as a separate
  process, never linked.
- The Python dependencies in `controller/requirements.txt`, all permissive.

## Wake word models

The stock models shipped by openWakeWord keep their upstream licence. Models
built with `oww_forge/` are generated from synthetic speech; see
`oww_forge/README.md`, whose pinned upstreams carry their own terms.
