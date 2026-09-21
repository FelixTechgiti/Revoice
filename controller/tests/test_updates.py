"""
The aggregated update answer, and the collapse it exists to prevent.

`em_updates` folds four independently versioned things into one indicator, and
the whole risk of doing that is arithmetic: a count of "devices with updates"
silently reports every device whose version could not be read as a device with
nothing waiting. That is the answer that stops somebody looking, and it is
exactly how a device sat for days on an emOS that could not resolve a hostname
while every panel said it was fine.

So most of what is pinned here is the THREE-WAY split surviving aggregation.
"""

import em_netflash
import em_platform
import em_updates as u


# ── One track ────────────────────────────────────────────────────────────────

def test_the_three_answers():
    assert u.track("2.51.0-fx.1", "2.52.0-fx.1") == u.AVAILABLE
    assert u.track("2.52.0-fx.1", "2.52.0-fx.1") == u.CURRENT
    assert u.track("", "2.52.0-fx.1") == u.UNKNOWN
    assert u.track("2.52.0-fx.1", "") == u.UNKNOWN
    assert u.track(None, None) == u.UNKNOWN


def test_a_build_ahead_of_the_release_is_current_not_behind():
    """
    A dev checkout describes as v2.52.0-3-gabc1234, whose base parses equal to
    the tag. Reporting that as an update would put a permanent notice on a
    machine that is ahead of everything published.
    """
    assert u.track("v2.52.0-fx.1-3-gabc1234", "v2.52.0-fx.1") == u.CURRENT
    assert u.track("2.53.0-fx.1", "2.52.0-fx.1") == u.CURRENT


def test_an_unreadable_version_is_never_current():
    """The load-bearing asymmetry, stated on its own so it cannot be relaxed
    by a change that only looks at the happy path."""
    for bad in ("", None, "dev", "unknown", "latest"):
        assert u.track(bad, "2.52.0-fx.1") != u.CURRENT, bad


# ── The emOS track's fourth answer ───────────────────────────────────────────

def test_fireos_has_no_emos_to_update():
    assert u.emos_track(em_platform.FIREOS, None, "emos-v0.7.0-fx.1") \
        == u.NOT_APPLICABLE


def test_a_device_that_has_not_said_its_base_is_unknown_not_exempt():
    """
    `em_platform.android_userspace` resolves absence to Android, and that
    asymmetry is about PAYLOAD GATING — it keeps the existing fleet's
    behaviour. Borrowing it here would mark every pre-#255 device as having
    no emOS track, which is a claim rather than a default.
    """
    for absent in (None, ""):
        assert u.emos_track(absent, None, "emos-v0.7.0-fx.1") == u.UNKNOWN


def test_an_emos_device_that_cannot_report_its_version_is_unknown():
    assert u.emos_track(em_platform.EMOS, None, "emos-v0.7.0-fx.1") == u.UNKNOWN


def test_the_tag_prefix_is_stripped_on_both_sides():
    """
    `version.parse` answers None for `emos-v0.6.0-fx.1`, so comparing the
    stamped strings directly does not fail loudly — it reports "cannot tell"
    for every device for ever, and the indicator goes quiet instead of wrong.
    """
    assert u.emos_track(em_platform.EMOS, "0.6.0-fx.1", "emos-v0.7.0-fx.1") \
        == u.AVAILABLE
    assert u.emos_track(em_platform.EMOS, "emos-v0.7.0-fx.1", "emos-v0.7.0-fx.1") \
        == u.CURRENT


def test_it_agrees_with_the_panel_that_already_answered_this():
    """
    `em_netflash.update_status` is what the emOS Updates tab renders, and this
    module is a second reader of the same question. Two rules that can
    disagree is a panel saying "up to date" beside a row saying "update
    available" — so the agreement is pinned rather than assumed.
    """
    for cur, lat in [("0.6.0-fx.1", "emos-v0.7.0-fx.1"),
                     ("0.7.0-fx.1", "emos-v0.7.0-fx.1"),
                     ("", "emos-v0.7.0-fx.1"),
                     ("0.7.0-fx.1", "")]:
        panel = em_netflash.update_status(cur, lat)
        mine = u.emos_track(em_platform.EMOS, cur, lat)
        if not panel["comparable"]:
            assert mine == u.UNKNOWN, (cur, lat)
        else:
            assert (mine == u.AVAILABLE) == panel["available"], (cur, lat)


# ── One device ───────────────────────────────────────────────────────────────

def _summary(**kw):
    base = dict(firmware_ver="2.52.0-fx.1", firmware_latest="2.52.0-fx.1",
                base_os=em_platform.EMOS, emos_ver="0.7.0-fx.1",
                emos_latest="emos-v0.7.0-fx.1")
    base.update(kw)
    return u.device_summary(u.device_tracks(**base))


def test_a_device_with_nothing_waiting():
    s = _summary()
    assert s["state"] == u.CURRENT
    assert s["available"] == [] and s["unknown"] == []


def test_available_beats_unknown():
    """Something is definitely waiting, so that is what the row should say —
    the unknown is still carried, for the tooltip."""
    s = _summary(firmware_latest="2.53.0-fx.1", emos_ver=None)
    assert s["state"] == u.AVAILABLE
    assert s["available"] == [u.FIRMWARE]
    assert s["unknown"] == [u.EMOS]


def test_unknown_beats_current():
    """The one collapse that would make the whole aggregate lie."""
    s = _summary(emos_ver=None)
    assert s["state"] == u.UNKNOWN
    assert s["unknown"] == [u.EMOS]


def test_a_fireos_device_is_current_rather_than_unknown():
    """It has no emOS track at all, and a permanent question mark on a device
    about which there is no question is noise that trains people to ignore
    the indicator."""
    s = _summary(base_os=em_platform.FIREOS, emos_ver=None)
    assert s["state"] == u.CURRENT
    assert s["tracks"][u.EMOS] == u.NOT_APPLICABLE


def test_the_tracks_are_named_not_counted():
    """An OTA and a partition write are not interchangeable, so "2 updates"
    without saying which tells the reader nothing they can act on."""
    s = _summary(firmware_latest="2.53.0-fx.1", emos_latest="emos-v0.8.0-fx.1")
    assert s["available"] == [u.FIRMWARE, u.EMOS]


# ── The fleet ────────────────────────────────────────────────────────────────

def test_the_two_counts_never_double_count_a_device():
    """
    A device that has one update AND one unreadable track is already counted
    as needing an update. Counting it in both would make the numbers add to
    more than the fleet, which is the first thing a reader notices and the
    last thing they trust afterwards.
    """
    fleet = u.fleet_summary([
        _summary(firmware_latest="2.53.0-fx.1", emos_ver=None),  # both
        _summary(emos_ver=None),                                  # unknown
        _summary(),                                               # current
    ])
    assert fleet == {"devices": 1, "unknown": 1, "total": 3,
                     "tracks": [u.FIRMWARE]}


def test_the_fleet_names_which_tracks_are_waiting():
    fleet = u.fleet_summary([
        _summary(firmware_latest="2.53.0-fx.1"),
        _summary(emos_latest="emos-v0.8.0-fx.1"),
    ])
    assert fleet["devices"] == 2
    assert fleet["tracks"] == [u.FIRMWARE, u.EMOS]


def test_track_order_is_stable_rather_than_alphabetical():
    """
    Sorted by DEVICE_TRACKS so a row and the fleet line list them the same
    way. Alphabetically "emos" would come first in one place and the
    declaration order in another, which reads as two different answers.
    """
    fleet = u.fleet_summary([
        _summary(emos_latest="emos-v0.8.0-fx.1"),
        _summary(firmware_latest="2.53.0-fx.1"),
    ])
    assert fleet["tracks"] == list(u.DEVICE_TRACKS)


def test_an_empty_fleet_is_zero_and_not_unknown():
    assert u.fleet_summary([]) == {"devices": 0, "unknown": 0, "total": 0,
                                   "tracks": []}
