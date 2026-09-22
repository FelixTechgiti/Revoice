"""
Re-flashing a device's boot partition over the network, and when to refuse.

A device on emOS has a root shell the controller can already reach and a
kernel that is not going to change, so replacing its emOS image needs no USB,
no TWRP and nobody in the room. That is what this module decides; `em_api`
carries it out.

**Why this is a different kind of write from the wizard's, and safer.**
The wizard writes the boot partition of a device running Amazon's Android,
and there the target is the hard part: amonet v1 inverts the by-name map, so
`/dev/block/by-name/boot_a` is p10 under TWRP and **p17 — amonet's unlock
payload — under Android** (measured 2026-08-08, the table in
`dashboard.jsx`). Writing a kernel there costs the unlock. The wizard's whole
`classifyBootTarget` apparatus exists for that one question, and going through
recovery is how it gets an answer it can trust.

None of that applies here. emOS resolves nothing by name: its own rollback
restores `/data/emos/boot-good.img` to `/dev/block/mmcblk0p10` by fixed node
(`BOOTDEV` in `emos/init/init.c`), and that node is the partition the running
system booted from. We write where emOS already writes.

**The rollback is the safety net, and it is the reason this is allowed at
all.** init confirms a boot only when the NETWORK COMES UP, and after three
unconfirmed boots it restores `boot-good.img` and shows an amber ring. So the
failure this feature could cause — an image that boots but cannot reach the
controller — is precisely the failure emOS already repairs by itself. That is
why `no_good_image` below is a refusal rather than a warning: without that
file the net is gone, and a network flash without a net is a worse trade than
a cable.

Pure and dependency-free, for the reason `em_platform.android_userspace` and
`em_linkauth.decide` are: every rule here is a refusal whose absence is
silent and whose cost is a device somebody has to open a case to recover.
"""

import re

import em_platform

# What the device must have free on /data before we stage an image there.
#
# The image is written to /data and then dd'd, so the partition briefly holds
# the staged copy on top of boot-good.img. Sized at twice a 16MB partition
# plus slack rather than at the image's measured size: the number that must
# not be wrong is a floor, and a floor computed from the thing being
# transferred moves every time the init grows.
MIN_FREE_MB = 48

# Where the pieces live on an emOS device. Mirrored from emos/init/init.c —
# BOOTDEV and GOODIMG there — and pinned by test, because a typo in either is
# a write to the wrong place or a rollback net reported present when it is
# absent.
BOOT_DEV = "/dev/block/mmcblk0p10"
GOOD_IMG = "/data/emos/boot-good.img"

# Staged here rather than in /tmp: /tmp on emOS is a tmpfs, so a 16MB image
# there is 16MB of the device's 512MB of RAM, and an interrupted flash would
# lose the staged copy on the reboot that follows.
STAGE_IMG = "/data/emos/new-boot.img"


class Refusal:
    """Why a reflash must not proceed, in a form the caller can log and show.

    `code` is for machines and tests, `message` for the person who asked for
    the reflash and is now deciding what to do instead. Every message names
    what to do next, because "refused" with no route out is what makes an
    operator reach for the cable they were told they would not need.
    """

    __slots__ = ("code", "message")

    def __init__(self, code: str, message: str):
        self.code = code
        self.message = message

    def __repr__(self) -> str:                      # pragma: no cover
        return f"Refusal({self.code!r})"


def base_refusal(base_os):
    """The one question that can be answered without touching the device.

    Split out so the endpoint can ask it before queueing minutes of work, and
    so `preflight` and that early check can never disagree — a second copy of
    this comparison, written to let a caller skip the arguments it does not
    have yet, is how the two ends of a gate drift apart.
    """
    if base_os == em_platform.EMOS:
        return None
    return Refusal(
        "not_emos",
        f"This device reports its base as {em_platform.label(base_os)}, "
        "and a network reflash is emOS-only. On Android the node this "
        "writes to is not the one the by-name map points at — amonet's "
        "unlock payload lives there — so the first install has to go "
        "through the wizard and TWRP. Once the device is on emOS, this "
        "works.")


def preview(base_os, good_image_present: bool, free_mb):
    """The refusals answerable WITHOUT reading the device's boot image.

    Split out so the dashboard can say why a reflash is not on offer before
    anybody clicks, using the same rules and the same words as the flash
    itself — `preflight` below is this plus the two checks that need the
    image. A second copy of these three, written so a preview could skip the
    arguments it does not have, is exactly how the button and the endpoint
    would come to disagree about whether a device is eligible.

    Returns None when nothing here objects; that is not yet permission.
    """
    # FIRST, and the only one whose absence would be catastrophic rather than
    # merely wrong. On Android the fixed node this writes to is not the
    # partition the by-name map points at, and the wizard's target
    # classification — the thing that knows the difference — is in the
    # browser, not here. A device that has not told us its base is refused
    # too: em_platform resolves absence toward Android for exactly this
    # reason, and the one place that asymmetry must not be softened is a
    # partition write.
    refusal = base_refusal(base_os)
    if refusal is not None:
        return refusal

    # The net. Without it an image that boots but cannot reach the network is
    # a device somebody has to fetch a cable for, which is the whole thing
    # this feature promises not to need.
    if not good_image_present:
        return Refusal(
            "no_good_image",
            f"The device has no {GOOD_IMG}, so emOS's rollback has nothing to "
            "restore and a failed flash would need a cable after all. init "
            "writes that file on a confirmed boot, so a device that has been "
            "up and on the network has one — reboot it and let it settle "
            "before trying again.")

    if free_mb is not None and free_mb < MIN_FREE_MB:
        return Refusal(
            "low_space",
            f"Only {free_mb}MB free on /data, and staging an image needs at "
            f"least {MIN_FREE_MB}MB. Clear some space — saved utterances and "
            "old crash logs are the usual occupants — and try again.")

    return None


def preflight(base_os, good_image_present: bool, free_mb,
              reference_head: bytes, init_arch):
    """Everything that must hold before a single byte is written.

    Returns None when the reflash may proceed, and a `Refusal` otherwise.
    Ordered so the cheapest and most decisive question is asked first: a
    device on FireOS is refused before anything is read off it.

    Called TWICE by design, either side of the transfer. The architecture can
    only be read from the whole kernel, and the kernel is eleven megabytes
    that there is no point moving for a device we are going to refuse — so
    everything else is asked first, off the 64-byte header.

    Two of the arguments carry a three-way answer, and collapsing either to a
    boolean is how this gate would quietly stop gating:

    - `free_mb` of None means the check could not run — `df` unreadable, or a
      busybox without it. That is NOT evidence of a full partition and must
      not refuse, the same reading the OTA free-space check applies. An
      actual number below the floor is evidence and does refuse.
    - `init_arch` of None means NOT ASKED YET, and is skipped. `""` means
      asked and unanswerable, and refuses. They are one keystroke apart and
      mean opposite things: the first is the pre-transfer call, the second is
      a kernel we could not identify, which is a device that would boot to
      silence.
    """
    # Everything that does not need the image, in one place shared with the
    # dashboard's preview.
    refusal = preview(base_os, good_image_present, free_mb)
    if refusal is not None:
        return refusal

    # Read off the device's own image rather than assumed, for the reason
    # `reference_kernel_arch` gives: the image is the only thing that knows,
    # and an init of the wrong architecture boots to nothing at all with no
    # output, which is indistinguishable from a kernel that never started.
    if len(reference_head) < 8 or reference_head[:8] != b"ANDROID!":
        return Refusal(
            "not_boot_image",
            f"What came back from {BOOT_DEV} is not an Android boot image. "
            "Nothing has been written. That is a read failing rather than a "
            "device being wrong, so it is worth simply trying again.")

    if init_arch is not None and not init_arch:
        return Refusal(
            "unknown_arch",
            "Could not tell which architecture this device's kernel is, so "
            "there is no way to choose an init for it. Empty is not evidence "
            "of either, and guessing here costs a device that boots to "
            "silence — so nothing has been written.")

    return None


# A digest is 32 hex characters and nothing else. Anything else on that line
# is the device talking — "md5sum: not found", a dd error, a shell complaint —
# and the difference decides what to do next, so it is matched rather than
# compared.
_MD5_RE = re.compile(r"^[0-9a-f]{32}$")


def read_back_verdict(out: str, sent_md5: str):
    """What the read-back actually said: ("ok"|"different"|"unreadable", got).

    Three answers where the caller used to get two, for the reason a digest of
    no bytes was split from corruption in `_pull_range_from_device` on the same
    day: **after a write, "the partition holds something else" and "we could
    not measure the partition" want opposite next moves.** The first is a bad
    write and the image has to be put back; the second says nothing about the
    partition at all, and treating it as a bad write would mean writing again
    on the strength of a missing tool.

    `got` is what the device put on its last line, and it rides the message.
    It is the only thing that can name a cause, and throwing it away is what
    made the first two failures of this feature unattributable.

    The digest is the LAST line: dd writes its block counts to stderr, which
    the command redirects, but a shell that says anything else first would
    otherwise be compared instead of the md5.

    What is compared is the image's own LENGTH read back off the partition,
    never the partition — `read_back_cmd` trims to it. That distinction is
    not pedantry: it is the bug that made every emOS flash in the wizard fail
    on a write dd reported as complete, because 425,984 bytes of the PREVIOUS
    image were being weighed against zero padding nobody had written.
    """
    got = ""
    for line in (out or "").strip().splitlines():
        line = line.strip()
        if line:
            got = line
    if not _MD5_RE.match(got.lower()):
        return "unreadable", got
    if not sent_md5:
        return "unreadable", got
    return ("ok" if got.lower() == sent_md5.strip().lower() else "different"), got


def read_back_cmd(length: int) -> str:
    """The device-side command that reads back exactly what we wrote.

    `bs=1 count=N` would be exact and unbearably slow on this hardware, so:
    whole 64K blocks through dd, and the length trimmed with `busybox head -c`,
    which costs one pipe.

    **The `busybox` was missing here until #320, and this docstring was why it
    looked right**: it said "which busybox has", and busybox does have it — but
    the command said only `head`, and on FireOS 6 a bare `head` is toybox,
    which has no `-c`. The reasoning named the right tool and the code did not.
    """
    blocks = (length + 65535) // 65536
    return (f"busybox dd if={BOOT_DEV} bs=65536 count={blocks} 2>/dev/null "
            f"| busybox head -c {length} | busybox md5sum | cut -d' ' -f1")


# Its own sentinel rather than PROBE_MARK's: this probe runs at the worst
# moment the feature has — the partition has been written and did not verify —
# and an empty answer there must read as "could not ask", never as a good
# image that is absent or empty.
GOOD_MARK = "_GOODCHK"


def good_image_cmd() -> str:
    """Size and digest of the rollback image, for verifying a restore.

    Asked only on the failure path, so a healthy flash pays nothing for it.
    Both values come from the device rather than from anything remembered
    here: the controller has never held a copy of this image and must not
    start — it is the user's own boot partition, and the one rule this whole
    feature is built around is that we do not keep one.
    """
    return (f"echo \"SIZE:$(busybox stat -c %s {GOOD_IMG} 2>/dev/null)\"; "
            f"echo \"MD5:$(busybox md5sum {GOOD_IMG} 2>/dev/null "
            f"| cut -d' ' -f1)\"; "
            f"echo {GOOD_MARK}")


def good_image(out: str):
    """Parse `good_image_cmd`. None when the probe did not run or is unusable.

    A restore that cannot be verified is not a restore — it is a second
    unverified write on top of the first — so anything missing here collapses
    to None and the caller says so instead of writing again.
    """
    text = out or ""
    if GOOD_MARK not in text:
        return None
    size, digest = None, ""
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("SIZE:"):
            raw = line[5:].strip()
            size = int(raw) if raw.isdigit() else None
        elif line.startswith("MD5:"):
            digest = line[4:].strip().lower()
    if not size or not _MD5_RE.match(digest):
        return None
    return {"size": size, "md5": digest}


def stage_size_cmd() -> str:
    """Ask how big the staged image actually is, with a sentinel.

    A bare `stat` answers nothing distinguishable from a shell that never
    opened, which is the failure `_SHELL_OK` and `_CLEARCHK` exist for
    elsewhere. Here the cost of that confusion would be writing a file of
    unknown size to the boot partition.
    """
    return (f"echo \"SIZE:$(busybox stat -c %s {STAGE_IMG} 2>/dev/null)\"; "
            f"echo _STAGECHK")


def stage_size(out: str):
    """Parse `stage_size_cmd`'s answer. None when the probe did not run.

    Without the sentinel an empty string would parse as "the file is not
    there", which is indistinguishable from "no shell", and those want
    opposite responses — retry versus do not write.
    """
    if "_STAGECHK" not in out:
        return None
    for line in out.splitlines():
        if line.startswith("SIZE:"):
            raw = line[5:].strip()
            return int(raw) if raw.isdigit() else None
    return None


def flash_cmd() -> str:
    """The write itself.

    `conv=fsync` rather than a trailing `sync`: the write must be on the flash
    before anything reads it back, and a separate sync is a second command
    that can be the one that does not run.

    **BUSYBOX's dd, and that is the whole of why the first network flash did
    not write anything.** A bare `dd` on either base is Amazon's toolbox
    binary out of `/system` — emOS mounts that filesystem, so the tool is
    present and answers, which is what makes this class of fault silent. Its
    dd is the NetBSD one and `conv=fsync` is a GNU/busybox extension it
    rejects outright: measured 2026-09-20, the write returned in **13
    milliseconds** for a 6.9MB image and the read-back then honestly reported
    a partition that had not changed. Same shape as `base64 -w0` and as
    `reboot` two steps below — a flag or a tool is not supported because the
    name resolves.
    """
    return f"busybox dd if={STAGE_IMG} of={BOOT_DEV} bs=65536 conv=fsync 2>&1"


def restore_cmd() -> str:
    """Put the known-good image back, byte for byte the same way.

    Reached when the partition does not read back as what was sent. The
    alternative — leaving it — is the one outcome emOS's own rollback cannot
    repair: that rollback lives in the init INSIDE the image being replaced,
    so a partition holding half of something never runs the code that would
    restore it. The device is still up and still reachable at that moment,
    which is the only window in which this is cheap.
    """
    return f"busybox dd if={GOOD_IMG} of={BOOT_DEV} bs=65536 conv=fsync 2>&1"


# `dd` reports its own work on the last line — busybox prints
# `6928384 bytes (6.6MB) copied, 0.352 seconds, 18.7MB/s`. That number is the
# only account of the write anybody gets.
_COPIED_RE = re.compile(r"(\d+)\s+bytes[^\n]*copied", re.IGNORECASE)


def wrote_bytes(out: str):
    """How many bytes `dd` says it wrote, or None when it did not say.

    None is NOT zero and the distinction decides whether the partition is
    presumed touched: a dd whose report we cannot read may have written
    everything, so the caller must go on and verify. Zero, or a short count,
    is dd telling us the write did not complete — and a short count is the
    state that needs the image put back.
    """
    m = None
    for m2 in _COPIED_RE.finditer(out or ""):
        m = m2
    return int(m.group(1)) if m else None


# ── Is this device's emOS behind the newest release? ──────────────────────────

# What both sides of the comparison carry in front of the number: emOS stamps
# `VERSION="emos-v0.5.0-fx.1"` into /etc/os-release at build time, and the
# release it would be compared against is the tag `emos-v0.6.0-fx.1`.
TAG_PREFIX = "emos-v"


def strip_tag(v):
    """The bare version, with the namespace prefix removed.

    Load-bearing rather than cosmetic: `version.parse` answers **None** for
    `emos-v0.6.0-fx.1` and a clean tuple for `0.6.0-fx.1`, so comparing the
    stamped strings directly does not fail loudly — it reports "cannot tell"
    for every device, for ever, and the Updates tab goes quiet instead of
    wrong. Measured 2026-09-20.
    """
    s = (v or "").strip()
    return s[len(TAG_PREFIX):] if s.startswith(TAG_PREFIX) else s


def update_status(current, latest):
    """Whether `current` is behind `latest`, as the dashboard should show it.

    Returns a dict with `current`, `latest`, `comparable` and `available`.

    **Absence is never "up to date".** A device whose version could not be
    read, and a controller that could not reach GitHub, both answer
    `comparable: False` — and the caller must render that as "unknown" rather
    than as the reassuring answer. The failure this avoids is the one the
    whole feature exists for: somebody looks at the tab, is told nothing is
    waiting, and keeps a device on an emOS that cannot resolve a hostname.

    Pure, and here rather than in `em_api`, so the prefix rule above and this
    asymmetry are both exercised by `tests/test_netflash.py` without a device,
    a release, or an event loop.
    """
    import version

    cur_s, lat_s = strip_tag(current), strip_tag(latest)
    cur, lat = version.parse(cur_s), version.parse(lat_s)
    if cur is None or lat is None:
        return {"current": cur_s, "latest": lat_s,
                "comparable": False, "available": False}
    return {"current": cur_s, "latest": lat_s,
            "comparable": True, "available": lat > cur}


# ── Asking the device what it is, once, for both readers ──────────────────────

# The sentinel that says the probe RAN. Without it an empty answer parses as
# "no rollback image and no free space", which is a refusal — and a device
# with no shell would be reported as a device that is not eligible, which are
# different problems with different next steps.
PROBE_MARK = "_RFCHK"


def probe_cmd() -> str:
    """Everything cheap the device can be asked before a reflash, in one go.

    One command and one round trip, because both callers want the same three
    answers and a second copy of this string is how the Updates tab and the
    reflash itself would come to disagree about whether a device is eligible.

    `df` output is returned RAW and parsed below rather than reduced with an
    awk field index: busybox wraps a long filesystem name onto its own line,
    so `$4` is the available column on one device and the use PERCENTAGE on
    another — which parses as no reading at all and silently retires the
    free-space check. Same rule, and same reason, as
    `em_oww_assets.parse_free_mb`.
    """
    return (f"[ -f {GOOD_IMG} ] && echo GOOD:yes || echo GOOD:no; "
            f"echo \"VER:$(sed -n 's/^VERSION=//p' /etc/os-release 2>/dev/null "
            f"| busybox tr -d '\\\"')\"; "
            # Both named, and both for #320. `df` is toybox on FireOS 6 and has
            # no `-m` at all (`usage: df [-HPkh]`), so this printed nothing and
            # the free-space check silently never ran. `tail` is worse: toybox
            # `tail -1` does not refuse the obsolescent count, it IGNORES it
            # and prints the whole stream — so even with df fixed, the parser
            # would have been handed a header row.
            #
            # Verified on hardware 2026-09-22, this exact pipeline:
            #   busybox df -m /data | busybox tail -1
            #     -> "     1224   178   1031  15% /data"
            #   free_from_df(...) -> 1031, and df's own Available is 1031.
            f"echo DF:$(busybox df -m /data 2>/dev/null | busybox tail -1); "
            f"echo {PROBE_MARK}")


def parse_probe(out):
    """Read `probe_cmd`'s answer.

    Returns None when the probe did not run at all — see PROBE_MARK. Otherwise
    a dict of `good_image`, `free_mb` (None when unreadable, which is NOT a
    refusal) and `emos_version` ("" when the stamp could not be read, which is
    not evidence of anything either).
    """
    text = out or ""
    if PROBE_MARK not in text:
        return None

    good, free_mb, ver = False, None, ""
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("GOOD:"):
            good = line[5:].strip() == "yes"
        elif line.startswith("VER:"):
            ver = line[4:].strip()
        elif line.startswith("DF:"):
            free_mb = free_from_df(line[3:])
    return {"good_image": good, "free_mb": free_mb, "emos_version": ver}


# Megabytes per unit suffix, for the `df` layout that prints them.
_DF_UNITS = {"K": 1 / 1024.0, "M": 1.0, "G": 1024.0, "T": 1024.0 * 1024.0}


def free_from_df(row):
    """Free megabytes from one `df -m` data row, on either layout busybox
    prints — and never from a left-hand column index.

    **Two layouts, and the device in front of us prints the one the existing
    reader could not see.** Measured on `G090L91180250AN1`, 2026-09-20:

        Filesystem    Size   Used   Free  Blksize
        /data       1010.8M 676.5M 334.3M   4096

    There is no `Use%` column at all, and the values carry unit suffixes. The
    classic layout, which `em_oww_assets.parse_free_mb` was written against,
    is the other one:

        /dev/block/x  1010    648    346   65%  /data

    So each layout is anchored on the thing that is stable IN IT, and neither
    on a field number counted from the left — which is the original trap, and
    what makes a wrapped filesystem name harmless here:

      - a `%` field means the classic layout, and available is the field
        before it;
      - otherwise the row ends `… Size Used Free Blksize`, so free is the
        SECOND-TO-LAST field. Counting from the right is what survives the
        wrap, because wrapping only ever removes fields from the left.

    Returns None when neither shape fits. None means "could not measure" and
    must not refuse — `preflight` is explicit that an unreadable `df` is not
    evidence of a full partition.
    """
    fields = (row or "").split()
    if len(fields) < 2:
        return None

    for i, f in enumerate(fields):
        if f.endswith("%") and i > 0:
            return _as_mb(fields[i - 1])

    return _as_mb(fields[-2])


def _as_mb(value):
    """A df cell as whole megabytes. None when it is not a measurement."""
    v = (value or "").strip()
    if not v:
        return None
    scale = 1.0
    if v[-1].upper() in _DF_UNITS:
        scale = _DF_UNITS[v[-1].upper()]
        v = v[:-1]
    try:
        return int(float(v) * scale)
    except ValueError:
        return None
