"""The release workflows must admit the tags this fork actually cuts.

Every fork release version carries `-fx.N` (repo-root CLAUDE.md, "Releasing on
this fork"), because upstream's tags are fetched in by the weekly sync and a
fork tag with an upstream name is a collision. The tag-triggered release
workflows guard the tag's shape before it reaches a shell, which is right — git
allows `$ ( ) ;` in a tag name — but upstream writes that guard against its own
suffix, `-ea.N`, and spells it out.

**That guard reached this fork in an upstream sync and refused v2.63.0-fx.1 on
2026-10-12**, eight seconds into the run, with the tag already created and
nothing published for it. Nothing caught it in between, because no release was
cut between the sync and that attempt — which is the shape worth a test rather
than a comment: a guard tightened upstream fails only at the moment it is
finally used, and that moment is a release.

So this checks both halves of what the guard is for: it admits what we cut, and
it still refuses anything that could reach a shell as something other than a
version.
"""

import re
from pathlib import Path

import pytest

WORKFLOWS = Path(__file__).resolve().parents[2] / ".github" / "workflows"

# The line is `echo "$GITHUB_REF_NAME" | grep -Eq '<pattern>' || {`.
_GUARD = re.compile(r"GITHUB_REF_NAME\"\s*\|\s*grep -Eq '([^']+)'")


def _pattern(workflow: str) -> str:
    text = (WORKFLOWS / workflow).read_text()
    found = _GUARD.findall(text)
    assert found, (
        f"{workflow} has no `grep -Eq` guard on GITHUB_REF_NAME. The tag is "
        "interpolated into later shell steps, so removing the guard is worse "
        "than narrowing it."
    )
    assert len(found) == 1, f"{workflow} has {len(found)} guards; expected one"
    return found[0]


# Real tags, as cut. The firmware ones come from this repository's own history;
# the `-ea.N` forms are upstream's and must keep working, since the sync brings
# upstream's releases in beside ours.
ACCEPTED = {
    "release.yml": [
        "v2.63.0-fx.1",   # the one that was refused
        "v2.62.0-fx.1",
        "v2.15.0-fx.1",
        "v2.18.0",        # upstream GA
        "v2.18.0-ea.3",   # upstream Early Access
        "v10.0.0-fx.12",
    ],
    "controller-release.yml": [
        "controller-v2.78.0-fx.1",
        "controller-v2.23.0-fx.1",
        "controller-v2.26.0",
        "controller-ea-v2.26.0-ea.3",
    ],
}

# Anything that is not a version, with the shell metacharacters git permits in
# a tag name among them. A guard that admits one of these is the reason the
# guard exists.
REFUSED = {
    "release.yml": [
        "v2.63.0-fx.1; rm -rf /",
        "v2.63.0-fx.1$(id)",
        "v2.63.0-fx.1`id`",
        "v2.63.0-fx.1 && echo",
        "v2.63",
        "2.63.0-fx.1",
        "controller-v2.63.0-fx.1",
        "emos-v0.10.0-fx.1",
        "endpoints-v1.20.0",
        "",
    ],
    "controller-release.yml": [
        "controller-v2.78.0-fx.1; id",
        "controller-v2.78.0-fx.1$(id)",
        "controller-v2.78",
        "v2.78.0-fx.1",
        "controller-ea-v2.78",
        "",
    ],
}


@pytest.mark.parametrize("workflow", sorted(ACCEPTED))
def test_the_guard_admits_every_tag_this_fork_cuts(workflow):
    pattern = re.compile(_pattern(workflow))
    for tag in ACCEPTED[workflow]:
        assert pattern.fullmatch(tag), (
            f"{workflow} refuses {tag!r}. The suffix must not be enumerated: "
            "this fork cuts -fx.N and upstream cuts -ea.N, and the guard is "
            "about the character class, not about which suffixes exist today."
        )


@pytest.mark.parametrize("workflow", sorted(REFUSED))
def test_the_guard_still_refuses_what_it_is_for(workflow):
    pattern = re.compile(_pattern(workflow))
    for tag in REFUSED[workflow]:
        assert not pattern.fullmatch(tag), (
            f"{workflow} accepts {tag!r}, which reaches a shell in a later "
            "step. Widening the suffix must not widen the character class."
        )


def test_cut_release_and_the_workflows_agree_on_what_a_version_is():
    """`cut-release.yml` builds the tag; these two consume it.

    A version `cut-release` accepts and `release.yml` then refuses is a tag
    created with nothing published for it, and a tag cannot be moved once a
    workflow has acted on it.
    """
    cut = (WORKFLOWS / "cut-release.yml").read_text()
    m = re.search(r"grep -qE '(\^\[0-9\].*?)'", cut)
    assert m, "cut-release.yml no longer validates the version it is given"
    version = re.compile(m.group(1))
    firmware = re.compile(_pattern("release.yml"))
    controller = re.compile(_pattern("controller-release.yml"))
    for v in ("2.63.0-fx.1", "2.26.0", "2.26.0-ea.3", "10.0.0-fx.12"):
        assert version.fullmatch(v), f"cut-release.yml would refuse {v!r}"
        assert firmware.fullmatch("v" + v), (
            f"cut-release.yml accepts {v!r} and release.yml refuses the tag it "
            "would create"
        )
        assert controller.fullmatch("controller-v" + v), (
            f"cut-release.yml accepts {v!r} and controller-release.yml refuses "
            "the tag it would create"
        )
