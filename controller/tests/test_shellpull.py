"""
Reading a file back OFF a device, and the two ways that goes wrong quietly.

This is the only path in the controller that pulls rather than pushes, so it
has no fleet behind it proving it works: the first thing ever to run it was
the emOS network reflash, against one Echo. Both faults it has had so far
were the same shape — the command worked at 64 bytes, failed at a megabyte,
and reported the failure as something about the DEVICE. So the tests here are
about the command text and about what each malformed answer is called, and
they run the shipping function rather than a copy of it.
"""

import asyncio
import base64
import hashlib
import time
from pathlib import Path
from shlex import quote

import apisrc

CONTROLLER = Path(__file__).resolve().parents[1]
CHUNK = 1 << 20


def _load():
    """The real `_pull_range_from_device`, with the real constants above it.

    The constants are exec'd out of em_api rather than restated here: a test
    holding its own copy of the sentinels would pass while the device and the
    parser used different ones.
    """
    src = (CONTROLLER / "em_api.py").read_text()
    head = src[src.index("PULL_CHUNK = 1 << 20"):
               src.index("async def _pull_range_from_device")]
    ns = {"asyncio": asyncio, "base64": base64, "hashlib": hashlib,
          "time": time, "_sh_quote": quote}
    exec(head + apisrc.extract("_pull_range_from_device"), ns)
    return ns


NS = _load()
PULL = NS["_pull_range_from_device"]
BEGIN, EOB, DONE = NS["_PULL_BEGIN"], NS["_PULL_EOB"], NS["_PULL_DONE"]


class Shell:
    """A device shell that answers one pull command with a scripted reply."""

    def __init__(self, reply, eol="\n"):
        self.reply = reply
        self.eol = eol
        self.sent = None
        self._left = None

    async def send(self, cmd):
        self.sent = cmd
        self._left = [self.eol.join(self.reply) + self.eol]

    async def recv(self):
        if not self._left:
            await asyncio.sleep(0.01)
            return ""
        return self._left.pop(0)


def answer(data: bytes, digest: str = None, body: str = None):
    """What a healthy device prints, with either half optionally sabotaged."""
    b64 = base64.b64encode(data).decode() if body is None else body
    md5 = hashlib.md5(data).hexdigest() if digest is None else digest
    return [BEGIN, b64, EOB, f"MD5:{md5}", DONE]


def pull(shell, length, path="/dev/block/mmcblk0p10", offset=0, timeout=1.0):
    # asyncio.run, the idiom the rest of these tests use: get_event_loop
    # borrows whatever loop a previously imported test left behind, which
    # passes alone and fails in the suite.
    return asyncio.run(PULL(shell, path, offset, length, timeout))


# ── The command ──────────────────────────────────────────────────────────────

def _command_for(length, offset=0):
    shell = Shell(answer(b"\0" * length))
    pull(shell, length, offset=offset)
    return shell.sent


def test_the_chunk_never_passes_through_a_shell_variable():
    """The fault that refused every reflash at offset 0.

    `printf %s "$__R"` with 1.37MB of base64 in it is past MAX_ARG_STRLEN for
    any printf that is a binary. The bytes were fine; only the digest step
    died, and the caller called that corruption.
    """
    cmd = _command_for(CHUNK)
    assert "printf" not in cmd
    assert "__R" not in cmd
    assert "base64 -d" not in cmd


def test_the_digest_comes_from_its_own_read_of_the_same_range():
    cmd = _command_for(CHUNK)
    assert cmd.count("dd if=") == 2
    assert "md5sum" in cmd
    # Both reads must name the same range, or the digest is of other bytes.
    reads = [part for part in cmd.split("dd if=")[1:]]
    assert reads[0].split("|")[0] == reads[1].split("|")[0]


def test_the_range_arithmetic_is_in_whole_chunks_and_trimmed_by_head():
    cmd = _command_for(CHUNK)
    assert f"bs={CHUNK} skip=0 count=1" in cmd
    assert f"head -c {CHUNK}" in cmd

    tail = _command_for(500_000, offset=2 * CHUNK)
    assert f"bs={CHUNK} skip=2 count=1" in tail
    assert "head -c 500000" in tail


def test_the_path_is_quoted():
    shell = Shell(answer(b"x" * 8))
    pull(shell, 8, path="/dev/block/a b")
    assert "'/dev/block/a b'" in shell.sent


# ── The answer ───────────────────────────────────────────────────────────────

def test_a_healthy_answer_returns_the_bytes():
    data = bytes(range(256)) * 4
    raw, why = pull(Shell(answer(data)), len(data))
    assert why == ""
    assert raw == data


def test_carriage_returns_do_not_break_the_frame():
    data = b"emos" * 64
    raw, why = pull(Shell(answer(data), eol="\r\n"), len(data))
    assert why == ""
    assert raw == data


def test_a_digest_of_nothing_is_named_unverified_rather_than_corrupt():
    """The exact answer the live device gave, twice, on 2026-09-20.

    A complete, correct chunk with the md5 of an empty pipe behind it. Calling
    that corruption sends the next person to look at the transfer, which is
    the one part of it that was working.
    """
    data = b"\x7fELF" + b"\0" * 1020
    raw, why = pull(Shell(answer(data, digest=hashlib.md5(b"").hexdigest())),
                    len(data))
    assert raw is None
    assert "unverified" in why
    assert "corrupt" not in why


def test_bytes_that_disagree_with_the_digest_are_corrupt():
    data = b"\0" * 512
    other = hashlib.md5(b"something else").hexdigest()
    raw, why = pull(Shell(answer(data, digest=other)), len(data))
    assert raw is None
    assert "corrupt" in why


def test_a_short_read_is_reported_as_short_and_not_as_corruption():
    data = b"\0" * 512
    raw, why = pull(Shell(answer(data[:100])), len(data))
    assert raw is None
    assert "expected 512 bytes, got 100" == why


def test_an_unframed_answer_is_incomplete_rather_than_empty():
    raw, why = pull(Shell(["MD5:" + hashlib.md5(b"").hexdigest(), DONE]), 512)
    assert raw is None
    assert "incomplete" in why


def test_an_empty_range_says_the_device_returned_nothing():
    raw, why = pull(Shell([BEGIN, "", EOB, "MD5:x", DONE]), 512)
    assert raw is None
    assert "nothing" in why


def test_an_answer_that_never_ends_is_not_read_as_a_short_file():
    raw, why = pull(Shell([BEGIN, "AAAA"]), 3, timeout=0.05)
    assert raw is None
    assert why == "the read did not complete"


# ── The call site ────────────────────────────────────────────────────────────

def test_the_reflash_asks_the_device_with_the_shared_probe():
    """The eligibility check and the flash must ask the same question.

    The reflash kept its own copy, which read free space out of `awk '$4'` —
    the block size on this device's df layout, so the free-space refusal had
    never run on emOS at all.
    """
    # Comments stripped first: the paragraph above the call explains the awk
    # this replaced, and a guard that reads the explanation instead of the
    # code passes for the wrong reason — this tree's recurring source-guard
    # trap.
    src = "\n".join(line for line in apisrc.extract("_emos_reflash_steps").splitlines()
                    if not line.lstrip().startswith("#"))
    assert "em_netflash.probe_cmd()" in src
    assert "awk" not in src
