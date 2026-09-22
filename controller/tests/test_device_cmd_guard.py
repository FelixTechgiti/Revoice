"""#320: a tool is not the one you mean because its name is on PATH.

The emOS network reflash could not read a FireOS 6 device's boot partition,
and said the bytes were not valid base64. They were not base64 at all: the
command trimmed its chunk with a bare `head -c`, `head` on that device is
/system/bin/head which is toybox, and toybox's head has no `-c`. Its
complaint went to the same stream as the data — the shell plane is not a pty
— so the decoder was handed an error message and blamed the encoding.

Which binary answers a bare name is a property of the DEVICE, not of the
command: FireOS 5 ships toolbox, FireOS 6 ships toybox, emOS puts ours in
/sbin, and /system is mounted under all of them. So a device command names
the tool it means.

These are shape guards on the shipped source, the same posture as
test_dispatch_guard.py — the commands cannot be run here, and the property
that matters is textual.
"""

import re
from pathlib import Path

CONTROLLER = Path(__file__).resolve().parents[1]

# Where device commands are built. Not every module: a guard over the whole
# tree would catch the controller's own local subprocess calls, which run on
# the container's coreutils and are correct as they are.
SOURCES = ("em_api.py", "em_netflash.py", "em_devicediag.py")

# Applets whose flags differ between busybox, toybox and toolbox, so that a
# bare name is a coin toss. `head -c` is the one that cost a reflash; the
# others are here because they are one edit away from being used the same way.
#
# `df` and `tail` were held back at first because `em_netflash.probe_cmd`'s
# output is PARSED, so naming busybox there is not a no-op — it can change the
# very text something reads. They are in now because that was measured rather
# than assumed, on hardware 2026-09-22: `busybox df -m /data | busybox tail -1`
# gives a row `free_from_df` reads as 1031 MB, which is what df's own
# Available column says. Before it, `df -m` printed nothing at all (toybox df
# has no -m) and `tail -1` printed the whole stream (toybox tail ignores the
# obsolescent count instead of refusing it) — so the free-space gate had never
# once run on the platform it exists for.
#
# `sed` stays out: its one use reads /etc/os-release through a script that
# both implementations accept, and there is no measurement for it yet.
RISKY = ("head", "base64", "md5sum", "tr", "nc", "dd", "stat", "timeout",
         "df", "tail")


def _device_command_lines(name: str):
    """Lines that build a shell command for a DEVICE.

    Every one of them is built from f-string fragments, so an f-string prefix
    is what separates a command from prose ABOUT a command. Without that,
    this matched a docstring paragraph explaining a former implementation —
    a guard that fires on its own documentation is one somebody switches off.
    """
    for n, line in enumerate(
            (CONTROLLER / name).read_text().splitlines(), start=1):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if not re.search(r"""\bf["']""", stripped):
            continue
        if "|" in stripped or "2>/dev/null" in stripped:
            yield n, stripped


def test_no_device_command_invokes_a_risky_tool_by_bare_name():
    offenders = []
    for name in SOURCES:
        for n, line in _device_command_lines(name):
            for tool in RISKY:
                # The tool as a word, not preceded by `busybox ` and not part
                # of a longer name (`busybox` itself contains no match, and
                # `md5sum` must not match inside `busybox md5sum`).
                for m in re.finditer(rf"(?<![\w/-]){tool}\s+-", line):
                    before = line[:m.start()]
                    if before.rstrip().endswith("busybox"):
                        continue
                    # An absolute path is a named tool too — /system/bin/ping
                    # is deliberate and says so.
                    if before.rstrip().endswith("/"):
                        continue
                    offenders.append(f"{name}:{n}: {tool} in: {line}")

    assert not offenders, (
        "a device command names a tool that PATH resolves differently per "
        "platform. On FireOS 6 a bare `head` is toybox and has no -c, which "
        "is #320 — the reflash reported the partition unreadable. Write "
        "`busybox <tool>`:\n  " + "\n  ".join(offenders))


def test_the_guard_would_catch_the_bug_it_was_written_for():
    """A guard that cannot fail is a guard nobody can trust.

    This pins that the pattern actually matches the line as it was shipped,
    rather than passing because the regex matches nothing at all.
    """
    broken = ('read = (f"dd if={path} bs={PULL_CHUNK} skip={skip} "'
              ' f"count={count} 2>/dev/null | head -c {length}")')
    assert re.search(r"(?<![\w/-])head\s+-", broken)
    fixed = broken.replace("| head -c", "| busybox head -c")
    m = re.search(r"(?<![\w/-])head\s+-", fixed)
    assert m is not None and fixed[:m.start()].rstrip().endswith("busybox")
