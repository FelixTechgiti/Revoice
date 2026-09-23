"""
What an Echo calls itself in Spotify and in AirPlay (#309).

The guard that matters here is the last one: the fallback is applied at four
separate push sites, and a fifth added without it hands that device its serial
back with nothing failing.
"""
import re
from pathlib import Path

import em_endpoint_names as names


CONTROLLER = Path(__file__).resolve().parents[1]


def test_an_empty_name_becomes_the_label():
    out = names.resolve({"airplayName": "", "spotifyName": ""}, "Küche")
    assert out["airplayName"] == "Küche"
    assert out["spotifyName"] == "Küche"


def test_a_name_somebody_typed_is_never_overwritten():
    """The fallback is a default, not a policy."""
    out = names.resolve({"airplayName": "Hi-Fi", "spotifyName": ""}, "Küche")
    assert out["airplayName"] == "Hi-Fi"
    assert out["spotifyName"] == "Küche"


def test_a_blank_label_leaves_the_key_alone():
    """
    A label of nothing but spaces is not a name. An endpoint advertising a
    blank string is worse than one advertising a serial — at least a serial
    identifies the box.
    """
    for label in ("", "   ", None):
        out = names.resolve({"airplayName": ""}, label)
        assert out["airplayName"] == "", f"label {label!r}"


def test_an_absent_key_stays_absent():
    """
    The config push reads a missing key as "leave this alone", so inventing
    one here would start pushing a name at firmware that never asked for it.
    """
    assert names.resolve({"owwThreshold": 0.5}, "Küche") == {"owwThreshold": 0.5}


def test_the_caller_s_dict_is_not_edited():
    """
    Callers pass the EFFECTIVE config, which is also mirrored onto the live
    Device and returned to the dashboard. A fallback leaking into either would
    be indistinguishable from a value somebody typed.
    """
    cfg = {"airplayName": ""}
    names.resolve(cfg, "Küche")
    assert cfg["airplayName"] == "", "the fallback leaked into the stored picture"


def test_whitespace_around_a_label_is_stripped():
    assert names.resolve({"airplayName": ""}, "  Küche  ")["airplayName"] == "Küche"


def test_every_full_config_push_resolves_the_names():
    """
    The source guard, and the reason this file exists.

    A full push is `{"type": "config", **<var>}`; the two partial ones send a
    single `listeningAnim` key and are not a config at all. The resolution is
    a REBIND of that variable on the line above, rather than an expression
    inside the send — four other tests anchor on the exact text of those send
    lines, and a guard that forces them all to be re-anchored is a guard
    people delete.

    So the shape checked is: within the ten lines before each full push, the
    variable it spreads has been passed through the resolver.
    """
    pat = re.compile(r'\{"type": "config", \*\*(?P<var>[A-Za-z_]+)\}')
    offenders = []
    for fname in ("em_api.py", "em_controller.py"):
        lines = (CONTROLLER / fname).read_text().splitlines()
        for i, line in enumerate(lines):
            m = pat.search(line)
            if not m:
                continue
            var = m.group("var")
            if var == "kwargs":
                continue  # send_config(**kwargs) — a partial, by construction
            before = "\n".join(lines[max(0, i - 10):i])
            if re.search(rf"{var} = .*(_with_endpoint_names|em_endpoint_names\.resolve)",
                         before):
                continue
            offenders.append(f"{fname}:{i + 1}  {line.strip()}")
    assert not offenders, (
        "a full config push whose config has not been through "
        "em_endpoint_names — that device will announce its serial:\n  "
        + "\n  ".join(offenders))


def test_the_guard_finds_the_pushes_it_is_guarding():
    """A net that catches nothing looks identical to a net over nothing."""
    pat = re.compile(r'\{"type": "config", \*\*(?P<var>[A-Za-z_]+)\}')
    found = 0
    for fname in ("em_api.py", "em_controller.py"):
        for line in (CONTROLLER / fname).read_text().splitlines():
            m = pat.search(line)
            if m and m.group("var") != "kwargs":
                found += 1
    assert found == 4, (
        f"expected the four full config pushes, found {found} — if a push was "
        "added or removed, check it against the guard above before changing "
        "this number")


def test_the_module_is_imported_where_it_is_used():
    api = (CONTROLLER / "em_api.py").read_text()
    ctl = (CONTROLLER / "em_controller.py").read_text()
    assert "import em_endpoint_names" in api
    assert "import em_endpoint_names" in ctl
