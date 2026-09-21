"""
The emOS version's path from the image to the fleet list, at its five seams.

`em_updates` is unit-tested on its own; what cannot be unit-tested is whether
anything CARRIES the value, because the suite cannot import em_controller or
the firmware. That is the half worth guarding, and every seam here fails
SILENTLY when it breaks: the version simply reads as unknown, which is the
answer a device with no emOS also gives, so nothing goes red and no panel
looks wrong — it just stops being able to tell anyone that an update exists.

Which is the state this feature was added to end (#255).
"""

import re
from pathlib import Path

import em_platform

ROOT = Path(__file__).resolve().parents[2]
CONTROLLER = (ROOT / "controller" / "em_controller.py").read_text()
API = (ROOT / "controller" / "em_api.py").read_text()
DB = (ROOT / "controller" / "em_db.py").read_text()
DASHBOARD = (ROOT / "controller" / "static" / "dashboard.jsx").read_text()
PLATFORM_GO = (ROOT / "device" / "internal" / "platform" / "platform.go").read_text()
CONTROL_GO = (ROOT / "device" / "internal" / "client" / "control.go").read_text()
BUILD_SH = (ROOT / "emos" / "build.sh").read_text()


def test_the_wire_key_matches_at_both_ends():
    """
    A typo here is a version that is never reported, on every device, for
    ever — and it looks exactly like firmware that is too old to say. Same
    reasoning as the capability-string mirroring.
    """
    assert em_platform.VERSION_REGISTER_KEY == "emos_ver"
    assert f'"{em_platform.VERSION_REGISTER_KEY}": platform.Ver()' in CONTROL_GO


def test_the_firmware_reads_the_field_emos_actually_stamps():
    """
    `emos/build.sh` writes the os-release, `platform.Version` reads it. Two
    programs, one format, and nothing else connects them — a rename on either
    side is a value that silently stops being found.
    """
    assert "VERSION_ID=" in BUILD_SH
    assert 'strings.HasPrefix(line, "VERSION_ID=")' in PLATFORM_GO


def test_it_rides_the_register_message_and_not_the_stats_tick():
    """
    A static property of the boot, like `base_os` beside it — which shipped
    on the stats report for exactly one commit and cost 240s of OTA timeouts
    because its consumer asked ~30s earlier than the answer arrived.

    Pinned by proximity: the two keys are emitted from the same literal, so a
    later move of one past the other is caught here rather than in the field.
    """
    reg = CONTROL_GO.split('"base_os": platform.Base(),')
    assert len(reg) == 2, "base_os is no longer emitted where this test expects"
    assert '"emos_ver": platform.Ver()' in reg[1][:2000]


def test_the_controller_stores_it():
    assert "ALTER TABLE devices ADD COLUMN emos_ver TEXT" in DB
    assert "def set_device_emos_ver" in DB
    assert "db.set_device_emos_ver(device_id, _ver)" in CONTROLLER


def test_a_falsy_value_never_overwrites_a_stored_one():
    """
    FireOS reports an empty string and old firmware reports nothing, and
    writing that NULL would erase the last known version of a device whose
    `base_os` already says it is not on emOS today. A stale version beside a
    corrected base is harmless; an erased one cannot be recovered without the
    device being on emOS again.
    """
    block = CONTROLLER.split("device._emos_ver = _ver")[1][:300]
    assert re.search(r"if _ver:\s*\n\s*db\.set_device_emos_ver", block), (
        "the store is no longer guarded on a truthy value"
    )


def test_the_api_serves_the_stored_value_when_the_device_is_offline():
    """
    The whole point of storing it. A fleet is mostly offline at any moment,
    so a live-only answer would make the aggregated indicator go blank
    exactly when somebody looks at a fleet they have not touched in a week.
    """
    assert '"emosVer"' in API
    assert 'row["emos_ver"] if "emos_ver" in row.keys() else None' in API


def test_the_release_lookup_is_cached_for_the_devices_list():
    """
    `_fetch_latest_emos_release` has no cache of its own, and `/api/devices`
    is polled continuously by every open dashboard. Calling it directly there
    is one outbound GitHub request per poll per user — the background traffic
    the update-check interval exists to bound.
    """
    assert "async def _get_cached_emos_release" in API
    devices = API.split("async def _current_releases")[1].split("\n@auth")[0]
    assert "_get_cached_emos_release()" in devices
    assert "_fetch_latest_emos_release()" not in devices, (
        "the devices list calls the UNCACHED fetcher — one GitHub request per "
        "dashboard poll"
    )


def test_a_failed_poll_does_not_evict_a_good_answer():
    """
    `_github_releases` returns None for a network blip, and treating that as
    "no release exists" flips every device to unknown on one dropped request
    — an indicator that flickers is one people learn to ignore.
    """
    fn = API.split("async def _get_cached_emos_release")[1].split("\nasync def ")[0]
    assert re.search(r"if fresh:\s*\n\s*_emos_release_cache", fn), (
        "the cache is written unconditionally, so a failed poll clears it"
    )


def test_the_dashboard_reads_the_answer_rather_than_re_deriving_it():
    """
    The row used to compare `device.firmware_ver` against the release itself,
    which is a second copy of a rule the server now owns — so the row and the
    device's own Updates tab were free to disagree about one device.
    """
    summary = DASHBOARD.split("function fleetSummary(")[1].split("\n}")[0]
    assert "d.updates?.state === 'available'" in summary
    assert "firmware_ver" not in summary, (
        "fleetSummary compares versions again — em_updates owns that rule"
    )


def test_the_badge_has_a_distinct_mark_for_unknown():
    """
    The one collapse that would undo the feature. Blank is what "current"
    looks like, so rendering an unreadable version as blank tells somebody
    nothing is waiting on a device nobody could ask.
    """
    badge = DASHBOARD.split("function updateBadge(")[1].split("\n}\n")[0]
    assert "u.state === 'unknown'" in badge
    assert "updatesUnreadable" in badge
