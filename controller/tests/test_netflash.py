"""
The network reflash: what it refuses, and the arithmetic it shares.

Every test here stands in for something that is silent when wrong. A refusal
that stops refusing does not raise, does not log and does not fail CI — it
writes a boot partition. So each rule is pinned from both sides: that it
fires, and that it does not fire on the case next to it.
"""

import re
import struct
from pathlib import Path

import pytest

import em_emos_build
import em_netflash
import em_platform

REPO = Path(__file__).resolve().parents[2]


def _header(ksz: int, rsz: int, ssz: int = 0, psz: int = 2048,
            magic: bytes = b"ANDROID!") -> bytes:
    """A boot-image header with the four fields the length arithmetic reads."""
    h = bytearray(64)
    h[0:8] = magic
    struct.pack_into("<I", h, 8, ksz)
    struct.pack_into("<I", h, 16, rsz)
    struct.pack_into("<I", h, 24, ssz)
    struct.pack_into("<I", h, 36, psz)
    return bytes(h)


# ─── The length arithmetic, in three languages ───────────────────────────────

@pytest.mark.parametrize("ksz,rsz,ssz,want", [
    # Exact multiples of the page size: no padding anywhere.
    (2048, 2048, 0, 2048 + 2048 + 2048),
    # One byte over a page in each field rounds up to the next.
    (2049, 1, 0, 2048 + 4096 + 2048),
    # A realistic emOS image: ~7MB kernel, ~4MB ramdisk (the static init).
    (7_000_000, 4_063_624, 0, 2048 + 7_000_064 + 4_065_280),
])
def test_boot_image_len_matches_the_header(ksz, rsz, ssz, want):
    assert em_emos_build.boot_image_len(_header(ksz, rsz, ssz)) == want


@pytest.mark.parametrize("head", [
    b"",                                     # nothing read at all
    b"ANDROID",                              # short of the magic
    _header(2048, 2048)[:63],                # short of the header
    _header(2048, 2048, magic=b"ANDROIDX"),  # not a boot image
    _header(2048, 2048, psz=4096),           # a page size we do not understand
])
def test_boot_image_len_is_zero_on_anything_unclear(head):
    """Zero means 'read the whole partition', never 'empty'.

    A size optimisation must never be the reason a reflash cannot happen, so
    every uncertain answer costs transfer and nothing else.
    """
    assert em_emos_build.boot_image_len(head) == 0


def test_the_three_copies_agree_on_where_the_fields_are():
    """`init.c`, `dashboard.jsx` and `em_emos_build` read the same offsets.

    Each of the three is the only code that can answer the question where it
    stands — PID 1 has no Python, a browser has no controller — so the
    duplication is deliberate. What must not drift is the offsets, and
    nothing but this test compares them.
    """
    c_src = (REPO / "emos" / "init" / "init.c").read_text(encoding="utf-8")
    body = c_src[c_src.index("static long boot_image_len"):]
    body = body[:body.index("\n}")]
    # h + 8 / 16 / 24 / 36, in the same order the struct lays them out.
    assert re.findall(r"h \+ (\d+)", body) == ["8", "16", "24", "36"]
    assert 'memcmp(h, "ANDROID!", 8)' in body
    assert "ps != 2048" in body

    jsx = (REPO / "controller" / "static" / "dashboard.jsx").read_text(
        encoding="utf-8")
    fn = jsx[jsx.index("function _bootImageLength"):]
    fn = fn[:fn.index("\n  }")]
    # SORTED, not in order: the JSX reads the page size first so it can use it
    # as the rounding unit, where init.c and this module compare it against
    # 2048 and round by a constant. Which offset is read first is an
    # implementation choice; WHICH FOUR OFFSETS is the shared fact, and it is
    # the only thing that can drift into a wrong length.
    assert sorted(re.findall(r"getUint32\((\d+)", fn), key=int) == [
        "8", "16", "24", "36"]


# ─── The preflight refusals ──────────────────────────────────────────────────

GOOD_HEAD = _header(7_000_000, 4_063_624)


def _ok(**over):
    args = dict(base_os=em_platform.EMOS, good_image_present=True,
                free_mb=512, reference_head=GOOD_HEAD,
                init_arch=em_emos_build.ARCH_ARM64)
    args.update(over)
    return em_netflash.preflight(**args)


def test_a_healthy_emos_device_is_allowed():
    assert _ok() is None


@pytest.mark.parametrize("base_os", [
    em_platform.FIREOS,
    em_platform.UNKNOWN,
    None,             # firmware too old to report the field
    "",               # a value that survived a round trip as empty
    "EMOS",           # case matters; the wire value is lowercase
])
def test_anything_but_a_positive_emos_is_refused(base_os):
    """The asymmetry `em_platform` documents, at the one place it must hold.

    Absence resolves toward Android everywhere in this project. On a
    partition write that is not a default, it is the whole safety argument:
    on Android the fixed node written here is not the one the by-name map
    points at, and amonet's unlock payload lives there.
    """
    r = _ok(base_os=base_os)
    assert r is not None and r.code == "not_emos"


def test_a_device_with_no_rollback_image_is_refused():
    r = _ok(good_image_present=False)
    assert r is not None and r.code == "no_good_image"


def test_a_full_data_partition_is_refused():
    r = _ok(free_mb=em_netflash.MIN_FREE_MB - 1)
    assert r is not None and r.code == "low_space"


def test_a_free_space_probe_that_could_not_run_does_not_refuse():
    """None is 'we did not measure this', never 'this was zero'.

    The same reading the OTA free-space check applies to an unreadable df —
    and the rule the project states as degrading to the old behaviour rather
    than to a wrong answer.
    """
    assert _ok(free_mb=None) is None


def test_the_boundary_is_allowed():
    """Exactly the floor passes; the refusal is strictly below it."""
    assert _ok(free_mb=em_netflash.MIN_FREE_MB) is None


def test_something_that_is_not_a_boot_image_is_refused():
    r = _ok(reference_head=b"\x00" * 64)
    assert r is not None and r.code == "not_boot_image"


def test_an_architecture_not_yet_asked_is_not_an_architecture_that_failed():
    """None and "" are one keystroke apart and mean opposite things.

    The reflash calls preflight twice, either side of an eleven-megabyte
    transfer, because the architecture sniffer needs the kernel. Before the
    transfer the answer is None — not asked — and must not refuse. Collapsing
    the two would either refuse every reflash or accept a kernel nobody
    identified.
    """
    assert _ok(init_arch=None) is None
    assert _ok(init_arch="").code == "unknown_arch"


def test_the_base_check_is_one_function_both_ends_call():
    """The endpoint refuses before queueing work; preflight refuses again.

    Two copies of this comparison — one written to let the early caller skip
    arguments it does not have yet — is how the two ends of a gate drift
    apart, so there is one.
    """
    assert em_netflash.base_refusal(em_platform.EMOS) is None
    for base in (em_platform.FIREOS, em_platform.UNKNOWN, None, ""):
        early = em_netflash.base_refusal(base)
        full = _ok(base_os=base)
        assert early is not None
        assert early.code == full.code == "not_emos"
        assert early.message == full.message


def test_an_unreadable_architecture_is_refused_rather_than_guessed():
    """Empty is not evidence of either architecture.

    An init of the wrong architecture boots to nothing at all with no output,
    which is indistinguishable from a kernel that never started — the failure
    that cost five flashed images on the LibreEcho kernel.
    """
    r = _ok(init_arch="")
    assert r is not None and r.code == "unknown_arch"


def test_the_base_check_comes_before_everything_else():
    """A FireOS device is refused for being FireOS, not for anything downstream.

    Order matters because the later checks read things off the device, and
    there is no reason to touch a device we are going to refuse.
    """
    r = em_netflash.preflight(em_platform.FIREOS, False, 0, b"", "")
    assert r.code == "not_emos"


def test_every_refusal_says_what_to_do_next():
    """A refusal with no route out is what sends somebody for a cable."""
    for over in ({"base_os": em_platform.FIREOS},
                 {"good_image_present": False},
                 {"free_mb": 0},
                 {"reference_head": b"\x00" * 64},
                 {"init_arch": ""}):
        r = _ok(**over)
        assert len(r.message) > 80, r.code
        assert r.message.rstrip().endswith("."), r.code


# ─── The comparison that gates a reboot ──────────────────────────────────────

def test_a_matching_read_back_passes():
    assert em_netflash.written_correctly("ABC123", "abc123") is True


def test_a_read_that_did_not_happen_is_not_a_pass():
    """Empty is a read that failed, never a partition that matched.

    This is the wizard's lesson in a different file: a check that cannot run
    must not read as a pass.
    """
    assert em_netflash.written_correctly("", "abc123") is False
    assert em_netflash.written_correctly("abc123", "") is False


def test_a_mismatch_fails():
    assert em_netflash.written_correctly("abc123", "def456") is False


# ─── The device-side commands ────────────────────────────────────────────────

def test_the_read_back_is_trimmed_to_the_image_not_the_partition():
    """Verify the bytes you wrote, not the block that contains them.

    Comparing whole blocks is what made every wizard flash fail on a write dd
    reported as complete: 425,984 bytes of the previous image checked against
    zeros nobody had written.
    """
    cmd = em_netflash.read_back_cmd(11_065_344)
    assert "head -c 11065344" in cmd
    assert "count=169" in cmd          # ceil(11065344 / 65536)
    assert em_netflash.BOOT_DEV in cmd


def test_the_write_targets_the_node_emos_own_rollback_writes():
    """Mirrored from init.c's BOOTDEV, and pinned because a typo is a brick."""
    c_src = (REPO / "emos" / "init" / "init.c").read_text(encoding="utf-8")
    assert f'#define BOOTDEV   "{em_netflash.BOOT_DEV}"' in c_src
    assert f'#define GOODIMG   "{em_netflash.GOOD_IMG}"' in c_src


def test_the_flash_syncs_in_the_same_command_that_writes():
    """A separate sync is a second command that can be the one that fails."""
    assert "conv=fsync" in em_netflash.flash_cmd()


def test_the_staged_image_is_not_on_a_tmpfs():
    """/tmp is RAM on this device, and a reboot follows the write."""
    assert em_netflash.STAGE_IMG.startswith("/data/")


def test_the_stage_size_probe_carries_a_sentinel():
    assert "_STAGECHK" in em_netflash.stage_size_cmd()


def test_a_stage_probe_that_did_not_run_is_none_not_zero():
    """No shell and no file want opposite responses — retry versus refuse."""
    assert em_netflash.stage_size("") is None
    assert em_netflash.stage_size("SIZE:\n_STAGECHK") is None
    assert em_netflash.stage_size("SIZE:11065344\n_STAGECHK") == 11065344


# ── Is this device's emOS behind? ─────────────────────────────────────────────

def test_strip_tag_removes_the_namespace_prefix():
    # The whole reason this function exists: version.parse answers None for a
    # prefixed string, so comparing the stamped values directly reports
    # "cannot tell" for every device rather than failing loudly.
    assert em_netflash.strip_tag("emos-v0.6.0-fx.1") == "0.6.0-fx.1"
    assert em_netflash.strip_tag("0.6.0-fx.1") == "0.6.0-fx.1"
    assert em_netflash.strip_tag("  emos-v0.5.0  ") == "0.5.0"
    assert em_netflash.strip_tag(None) == ""


def test_update_status_compares_prefixed_versions():
    s = em_netflash.update_status("emos-v0.5.0-fx.1", "emos-v0.6.0-fx.1")
    assert s == {"current": "0.5.0-fx.1", "latest": "0.6.0-fx.1",
                 "comparable": True, "available": True}


def test_update_status_says_current_when_it_is():
    s = em_netflash.update_status("emos-v0.6.0-fx.1", "emos-v0.6.0-fx.1")
    assert s["comparable"] is True and s["available"] is False


def test_an_unreadable_version_is_never_reported_as_up_to_date():
    # The failure this guards: "nothing is waiting" is the reassuring answer,
    # and it must never be what a missing measurement produces.
    for cur, lat in (("", "emos-v0.6.0-fx.1"),
                     ("emos-v0.5.0-fx.1", ""),
                     ("", ""),
                     ("not-a-version", "emos-v0.6.0-fx.1")):
        s = em_netflash.update_status(cur, lat)
        assert s["comparable"] is False, (cur, lat)
        assert s["available"] is False, (cur, lat)


# ── The shared probe ──────────────────────────────────────────────────────────

def test_probe_parses_the_layout_a_real_device_prints():
    # Read off G090L91180250AN1 (emos-v0.5.0-fx.1) on 2026-09-20. Note there
    # is NO Use% column and the values carry unit suffixes — the layout the
    # percentage-anchored reader could not see.
    out = ("GOOD:yes\n"
           "VER:emos-v0.5.0-fx.1\n"
           "DF:/data       1010.8M  676.5M  334.3M  4096\n"
           "_RFCHK")
    assert em_netflash.parse_probe(out) == {
        "good_image": True, "free_mb": 334, "emos_version": "emos-v0.5.0-fx.1"}


def test_probe_parses_the_classic_layout_too():
    out = ("GOOD:no\nVER:\n"
           "DF:/dev/block/x 1010 648 346 65% /data\n_RFCHK")
    p = em_netflash.parse_probe(out)
    assert p == {"good_image": False, "free_mb": 346, "emos_version": ""}


def test_a_wrapped_filesystem_name_changes_nothing():
    # busybox wraps a long name onto its own line, so the data row loses its
    # leading field. Both layouts are anchored on something at the RIGHT, so
    # neither is affected.
    classic = em_netflash.parse_probe(
        "GOOD:yes\nVER:x\nDF:1010 648 346 65% /data\n_RFCHK")
    suffixed = em_netflash.parse_probe(
        "GOOD:yes\nVER:x\nDF:1010.8M 676.5M 334.3M 4096\n_RFCHK")
    assert classic["free_mb"] == 346
    assert suffixed["free_mb"] == 334


def test_a_probe_that_did_not_run_is_not_a_refusal():
    # No sentinel means no shell. That must not read as "no rollback image
    # and no free space", which is a device that looks ineligible.
    assert em_netflash.parse_probe("") is None
    assert em_netflash.parse_probe("bash: df: not found") is None


def test_an_unreadable_df_measures_nothing_rather_than_zero():
    p = em_netflash.parse_probe("GOOD:yes\nVER:x\nDF:\n_RFCHK")
    assert p["free_mb"] is None
    # And None must not refuse — preflight is explicit about that.
    assert em_netflash.preview("emos", True, None) is None


def test_free_from_df_scales_units():
    assert em_netflash.free_from_df("/data 4.0G 1.0G 2.5G 4096") == 2560
    assert em_netflash.free_from_df("/data 900K 100K 512K 4096") == 0
    assert em_netflash.free_from_df("nonsense") is None


# ── preview and preflight must not drift ──────────────────────────────────────

def test_preview_is_the_head_of_preflight():
    # Every case preview refuses, preflight must refuse identically — that is
    # the whole reason the dashboard may use preview to decide what to show.
    head = b"ANDROID!" + b"\0" * 56
    for base, good, free in (("fireos", True, 500), (None, True, 500),
                             ("emos", False, 500), ("emos", True, 1)):
        pv = em_netflash.preview(base, good, free)
        pf = em_netflash.preflight(base, good, free, head, None)
        assert pv is not None and pf is not None, (base, good, free)
        assert pv.code == pf.code, (base, good, free)
        assert pv.message == pf.message, (base, good, free)


def test_preview_passing_does_not_yet_mean_preflight_passes():
    # preview cannot see the boot image, so it must not be read as permission.
    assert em_netflash.preview("emos", True, 500) is None
    assert em_netflash.preflight("emos", True, 500, b"nope", None).code \
        == "not_boot_image"
