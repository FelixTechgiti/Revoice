"""
One answer to "is anything waiting?", across four things that update apart.

Revoice versions four things independently and updates each by a different
mechanism: the controller with the user's own `docker compose pull`, the
firmware by OTA, emOS by a partition write, and the streaming endpoints by a
file push. Before this module two of those were visible on the dashboard and
two were not, and the split was not a judgement about importance — it followed
what the controller happened to know cheaply. The emOS version in particular
cost a ~26s shell probe per device, which is affordable for a tab somebody
deliberately opened and impossible for a list, so it could not appear anywhere
a person would notice it (#255).

**The asymmetry this module exists to preserve: AVAILABLE, CURRENT and UNKNOWN
are three answers, not a boolean with an error case.** A device whose version
could not be read, and a device that is up to date, must never render alike.
That is the same rule `em_netflash.update_status` already insists on for one
device, and aggregating is precisely where it is easiest to lose: a count of
"devices with updates" quietly reports every unknown as a device with nothing
waiting, and the reader stops looking. So every function here carries the
unknowns alongside the availables and neither is derivable from the other.

Pure and dependency-free, for the reason `em_linkauth.decide` and
`em_platform.android_userspace` are: the rules are a handful of comparisons,
getting one backwards is silent, and they belong somewhere a test can reach
without aiohttp or a device.
"""

import em_netflash
import em_platform
import version

# ── The three answers ────────────────────────────────────────────────────────

AVAILABLE = "available"   # a strictly newer version exists
CURRENT = "current"       # running the newest, or ahead of it
UNKNOWN = "unknown"       # nobody could read one side of the comparison
NOT_APPLICABLE = "n_a"    # this track does not exist on this device

# ── The tracks ───────────────────────────────────────────────────────────────
#
# Per-device, because the answer differs per device. The controller and the
# endpoint store are fleet-level and are NOT here: there is one controller,
# and the store is one directory beside the database. Mixing them in would
# make "3 devices have updates" mean something different depending on which
# tracks happened to be counted.

FIRMWARE = "firmware"
EMOS = "emos"

DEVICE_TRACKS = (FIRMWARE, EMOS)


def track(current, latest) -> str:
    """
    One track's state, from a running version and the newest published one.

    UNKNOWN whenever either side is unparseable, which is `version.compare`'s
    own answer and the only honest one: a device that has never connected has
    no version, and a GitHub poll that failed has no release. Both are "nobody
    looked", and reporting either as CURRENT is the failure this whole module
    is shaped around.
    """
    result = version.compare(current or "", latest or "")
    if result["status"] == "unknown":
        return UNKNOWN
    return AVAILABLE if result["available"] else CURRENT


def emos_track(base_os, emos_ver, emos_latest) -> str:
    """
    The emOS track, which has a fourth answer the firmware track does not.

    NOT_APPLICABLE for a device that booted FireOS — it has no emOS to update
    and an "unknown" there would be a permanent question mark on a device
    about which there is no question. Absence of `base_os` does NOT reach it:
    firmware too old to report a base is a device we cannot classify, and
    `em_platform.android_userspace`'s asymmetry is about payload gating rather
    than about display. Here the honest answer is UNKNOWN.

    Versions are stripped of the `emos-v` tag prefix first. `version.parse`
    answers None for `emos-v0.6.0-fx.1` and a clean tuple for `0.6.0-fx.1`, so
    comparing the stamped strings directly does not fail loudly — it reports
    "cannot tell" for every device, for ever.
    """
    if base_os == em_platform.FIREOS:
        return NOT_APPLICABLE
    if not base_os:
        return UNKNOWN
    return track(em_netflash.strip_tag(emos_ver),
                 em_netflash.strip_tag(emos_latest))


def device_tracks(*, firmware_ver, firmware_latest,
                  base_os, emos_ver, emos_latest) -> dict:
    """Every per-device track, by name."""
    return {
        FIRMWARE: track(firmware_ver, firmware_latest),
        EMOS: emos_track(base_os, emos_ver, emos_latest),
    }


def device_summary(tracks: dict) -> dict:
    """
    One device's tracks folded into something a row can render.

    `available` and `unknown` are both LISTS of track names rather than
    counts, because the action differs per track — an OTA and a partition
    write are not interchangeable — so an indicator that says "2 updates"
    without saying which has told the reader nothing they can act on.

    `state` is the single worst answer, for a one-glance indicator:
    AVAILABLE beats UNKNOWN beats CURRENT. AVAILABLE first because something
    is definitely waiting; UNKNOWN above CURRENT because "I could not look"
    must not be absorbed into "nothing to do", which is the one collapse that
    makes the whole aggregate lie.
    """
    avail = [t for t in DEVICE_TRACKS if tracks.get(t) == AVAILABLE]
    unk = [t for t in DEVICE_TRACKS if tracks.get(t) == UNKNOWN]
    if avail:
        state = AVAILABLE
    elif unk:
        state = UNKNOWN
    else:
        state = CURRENT
    return {"tracks": tracks, "available": avail, "unknown": unk,
            "state": state}


def fleet_summary(summaries) -> dict:
    """
    The whole fleet in one line, from the per-device summaries.

    `devices` counts devices with at least one update, `unknown` counts
    devices where at least one track could not be read AND none was
    available — a device that is both is already counted as needing an
    update, and counting it twice would make the two numbers add to more
    than the fleet.

    `tracks` names which tracks are waiting anywhere, so the line can say
    "firmware and emOS" rather than a bare number. Sorted by DEVICE_TRACKS
    rather than alphabetically, so the order is stable and matches the order
    a device row shows them in.

    An EMPTY fleet answers zero for both, which is correct and worth saying:
    nothing is waiting because there is nothing to wait for, and that is not
    the same as the unknown a device with an unreadable version produces.
    """
    summaries = list(summaries)
    tracks = [t for t in DEVICE_TRACKS
              if any(s.get("tracks", {}).get(t) == AVAILABLE for s in summaries)]
    return {
        "devices": sum(1 for s in summaries if s.get("state") == AVAILABLE),
        "unknown": sum(1 for s in summaries if s.get("state") == UNKNOWN),
        "total": len(summaries),
        "tracks": tracks,
    }
