"""
Picking an endpoints release, and deciding what to overwrite.

Two failures this pins, both of which are silent on a live controller:

  * selecting by LIST ORDER rather than by parsed version. The tag prefix
    has to come off before version.parse sees it — `endpoints-v1.2.0` parses
    as None otherwise, every candidate sorts equal, and "newest" quietly
    becomes "first". Written and caught before it shipped, which is exactly
    why it is a test.
  * overwriting a hand-uploaded binary. Somebody testing a patched librespot
    would lose it to the next poll with nothing said.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import em_endpoint_bins as bins
import em_endpoint_release as rel


# Every kind a release is expected to carry, read off KINDS rather than
# written down again: the whole hazard `in_release` exists for is a list here
# and a list there drifting apart, and a fixture that hardcoded two asset
# names is what made adding a third kind look like sixteen unrelated failures.
ALL_ASSETS = tuple(k.filename for k in bins.KINDS.values() if k.in_release)


def _release(tag, *, names=ALL_ASSETS, prerelease=False):
    return {
        "tag_name": tag,
        "prerelease": prerelease,
        "assets": [{"name": n, "browser_download_url": f"https://x/{tag}/{n}",
                    "size": 10} for n in names],
    }


def test_the_newest_version_wins_not_the_first_in_the_list():
    picked = rel.select([_release("endpoints-v1.9.0"),
                         _release("endpoints-v1.10.0"),
                         _release("endpoints-v1.2.0")])
    assert picked["tag"] == "endpoints-v1.10.0"


def test_a_fork_suffix_does_not_change_the_ordering():
    picked = rel.select([_release("endpoints-v1.2.0-fx.1"),
                         _release("endpoints-v1.1.0")])
    assert picked["tag"] == "endpoints-v1.2.0-fx.1"


def test_a_release_carrying_one_binary_is_skipped_not_half_used():
    picked = rel.select([_release("endpoints-v2.0.0", names=("librespot",)),
                         _release("endpoints-v1.0.0")])
    assert picked["tag"] == "endpoints-v1.0.0"


def test_other_tag_namespaces_are_never_selected():
    assert rel.select([_release("v2.19.0-fx.1"),
                       _release("controller-v2.26.0-fx.1"),
                       _release("emos-v0.1")]) is None


def test_prereleases_are_skipped():
    assert rel.select([_release("endpoints-v1.0.0", prerelease=True)]) is None


def test_every_asset_is_returned_with_its_url():
    picked = rel.select([_release("endpoints-v1.0.0")])
    assert set(picked["assets"]) == {
        k.key for k in bins.KINDS.values() if k.in_release}
    assert picked["assets"]["spotify"]["name"] == "librespot"
    assert picked["assets"]["airplay"]["name"] == "shairport-sync"
    assert picked["assets"]["airplay2"]["name"] == "shairport-sync-ap2"
    assert picked["assets"]["nqptp"]["name"] == "nqptp"


# ─── needs_fetch ─────────────────────────────────────────────────────────────
#
# Built from KINDS rather than from two names written out, for the reason the
# fixture above is: these tests are about the PROVENANCE RULE, and a kind
# added later must not make them fail for having nothing to do with it.

KEYS = sorted(k.key for k in bins.KINDS.values() if k.in_release)


def _store(**overrides):
    """A store holding every published kind, minus or plus what is named."""
    have = {k: {"md5": f"md5-{k}"} for k in KEYS}
    have.update(overrides)
    return have


def _prov(tag):
    return {"tag": tag, "kinds": {k: {"md5": f"md5-{k}"} for k in KEYS}}


def test_an_empty_store_fetches_everything():
    assert sorted(rel.needs_fetch("endpoints-v1.0.0", {},
                                  {k: None for k in KEYS})) == KEYS


def test_our_own_copy_is_left_alone_while_the_tag_has_not_moved():
    tag = "endpoints-v1.0.0"
    assert rel.needs_fetch(tag, _prov(tag), _store()) == []


def test_our_own_copy_is_replaced_when_the_tag_moves():
    assert sorted(rel.needs_fetch("endpoints-v1.1.0",
                                  _prov("endpoints-v1.0.0"), _store())) == KEYS


def test_a_hand_uploaded_binary_is_never_overwritten():
    # The md5 in the store is not the one we recorded, so a person put it
    # there — a patched librespot being tested, most likely. Replacing it on
    # a timer would undo their build silently, and only that one is spared.
    got = rel.needs_fetch("endpoints-v1.1.0", _prov("endpoints-v1.0.0"),
                          _store(spotify={"md5": "HAND-BUILT"}))
    assert sorted(got) == [k for k in KEYS if k != "spotify"]


def test_no_provenance_means_the_store_is_not_ours_to_replace():
    assert rel.needs_fetch("endpoints-v1.1.0", {}, _store()) == []


# ─── published_state ─────────────────────────────────────────────────────────
#
# "The store has a binary" was the only thing anybody could see, and it hid the
# case that matters: a store filled before provenance existed looks exactly
# like one holding the current published build. The automatic fetch refuses to
# touch either, which is right for one of them and is why the OTA could never
# take over an existing installation.

PROV = {"tag": "endpoints-v1.0.0",
        "kinds": {"spotify": {"md5": "aaa"}, "airplay": {"md5": "bbb"}}}


def test_an_empty_slot_is_empty_not_unmanaged():
    got = rel.published_state("endpoints-v1.0.0", PROV,
                              {"spotify": None}, "spotify")
    assert got["state"] == "empty"


def test_our_own_current_copy_reads_as_published():
    got = rel.published_state("endpoints-v1.0.0", PROV,
                              {"spotify": {"md5": "aaa"}}, "spotify")
    assert got["state"] == "published"


def test_our_own_older_copy_reads_as_outdated_and_names_its_tag():
    got = rel.published_state("endpoints-v1.1.0", PROV,
                              {"spotify": {"md5": "aaa"}}, "spotify")
    assert got["state"] == "outdated"
    assert got["stored_tag"] == "endpoints-v1.0.0"


def test_a_binary_we_cannot_prove_we_wrote_reads_as_unmanaged():
    got = rel.published_state("endpoints-v1.0.0", PROV,
                              {"spotify": {"md5": "HAND-BUILT"}}, "spotify")
    assert got["state"] == "unmanaged"


def test_a_store_from_before_provenance_existed_is_unmanaged_too():
    # The case that makes this worth reporting: no record at all, which is
    # every installation that used the upload path before the fetch existed.
    # Indistinguishable from a deliberate upload, which is exactly why the
    # decision goes to the user rather than to needs_fetch.
    got = rel.published_state("endpoints-v1.0.0", {},
                              {"spotify": {"md5": "whatever"}}, "spotify")
    assert got["state"] == "unmanaged"


def test_no_release_still_reports_the_stored_state():
    # "We cannot see a release" and "the store is current" are different
    # answers, and only one of them means there is nothing to do.
    got = rel.published_state(None, PROV, {"spotify": {"md5": "aaa"}}, "spotify")
    assert got["state"] == "outdated"
