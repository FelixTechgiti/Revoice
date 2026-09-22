"""
Guard against the config-clobber trap.

Config POSTs REPLACE the stored dict rather than merging. That is safe for
the dashboard, which always submits the complete config, and a trap for any
caller that submits a partial one. On 2026-07-20 a POST carrying the single
key wakeArbitrationMs reset all 26 fleet settings to defaults: the wake
model reverted hey_mycroft -> hey_jarvis (so the real wake word stopped
working), owwThreshold dropped 0.5 -> 0.3 (so devices false-woke on ordinary
conversation), and AEC, barge-in, NS, beamforming, the BLE proxy and the EQ
curve all switched off.

These tests exercise the pure key-set logic directly rather than standing up
an aiohttp app — em_api pulls in the whole controller stack, which this
suite deliberately keeps out. The handler wiring is a two-line call into
this function on each of the two write paths.
"""

import re
from pathlib import Path

import pytest

CONTROLLER = Path(__file__).resolve().parents[1]


def _load_dropped_keys():
    """
    Extract _dropped_keys from em_api source and exec it in isolation.
    Importing em_api would drag in aiohttp/openwakeword; the function is
    self-contained (stdlib only), so this keeps the test honest without the
    dependency weight.

    Uses AST rather than a regex: a regex over function boundaries broke the
    moment a neighbouring decorator moved, and — worse — silently widened to
    swallow decorated handlers. Decorators are deliberately NOT applied here,
    which is exactly why the separate decorator-placement tests below exist.
    """
    import ast
    src = (CONTROLLER / "em_api.py").read_text()
    tree = ast.parse(src)
    for node in ast.walk(tree):
        if isinstance(node, ast.FunctionDef) and node.name == "_dropped_keys":
            node.decorator_list = []
            mod = ast.Module(body=[node], type_ignores=[])
            ns: dict = {}
            exec(compile(ast.fix_missing_locations(mod), "<em_api>", "exec"), ns)
            return ns["_dropped_keys"]
    raise AssertionError("could not locate _dropped_keys in em_api.py")


dropped_keys = _load_dropped_keys()

# The real fleet config as it stood before the incident.
LIVE_CONFIG = {
    "adcDigitalGain": 88, "adcMicpga": 40, "micGainDb": 24,
    "aecEnabled": True, "aecDelayMs": 0, "aecTailMs": 300,
    "startupVolume": 85, "vadThreshold": 0.001, "vadSpeechMs": 32,
    "vadSilenceMs": 900, "owwThreshold": 0.5, "bargeInEnabled": True,
    "bargeInThreshold": 0.05, "owwModel": "hey_mycroft_v0.1",
    "owwSpeexNs": False, "nsAsr": True, "bleProxyEnabled": True,
    "beamformingEnabled": True, "beamAngle": -1,
    "eqBands": [0, 3, 2, 0, -2, 3, 7, 0], "eqLoudness": True,
    "ledScene": "standard", "ledListenColor": "#00b400",
    "ledThinkColor": "#00c800", "agcEnabled": True, "nsEnabled": False,
}


def test_the_exact_body_that_caused_the_incident_is_caught():
    """The literal payload I sent on 2026-07-20."""
    dropped = dropped_keys({"wakeArbitrationMs": 700}, LIVE_CONFIG)
    assert len(dropped) == 26
    # The two that turned into audible symptoms.
    assert "owwModel" in dropped
    assert "owwThreshold" in dropped


def test_full_config_write_drops_nothing():
    """How the dashboard behaves — must stay a no-op."""
    body = dict(LIVE_CONFIG)
    body["wakeArbitrationMs"] = 700
    assert dropped_keys(body, LIVE_CONFIG) == []


def test_read_modify_write_is_the_safe_pattern():
    """The pattern the error message tells callers to use."""
    body = {**LIVE_CONFIG, "owwThreshold": 0.6}
    assert dropped_keys(body, LIVE_CONFIG) == []


def test_dropping_a_single_key_is_still_caught():
    body = {k: v for k, v in LIVE_CONFIG.items() if k != "aecEnabled"}
    assert dropped_keys(body, LIVE_CONFIG) == ["aecEnabled"]


def test_empty_stored_config_permits_anything():
    """First write on a fresh install has nothing to destroy."""
    assert dropped_keys({"owwThreshold": 0.5}, {}) == []


def test_result_is_sorted_for_a_stable_error_message():
    body = {"micGainDb": 24}
    out = dropped_keys(body, LIVE_CONFIG)
    assert out == sorted(out)


@pytest.mark.parametrize("path,handler", [
    ("global", "_post_global_config"),
    ("device", "_post_device_config"),
])
def test_both_write_paths_are_guarded(path, handler):
    """
    Both endpoints still run the check, and both still refuse.

    **The reason given here used to be "both endpoints replace rather than
    merge", and that was only ever true of the global one.** The per-device
    handler merges — `{**current, **values}` — so nothing in scope is lost by
    omission there. The check stays on both because a refusal is cheap and a
    silent clobber is not, but the per-device path also excludes STATE_KEYS,
    which is what #325 was: keys the dashboard has no field for, counted as
    deletions, making a used device's config unsaveable for good.
    """
    src = (CONTROLLER / "em_api.py").read_text()
    m = re.search(rf"async def {handler}\(.*?(?=\nasync def )", src, re.S)
    assert m, f"could not locate {handler}"
    body = m.group(0)
    assert "_dropped_keys(" in body, f"{handler} does not call _dropped_keys"
    assert "would_drop_keys" in body, f"{handler} does not refuse the write"


def test_new_default_key_does_not_block_a_stale_dashboard_save():
    """
    Regression for a false positive found while writing this guard.

    get_global_device_config() underlays DEFAULT_DEVICE_CONFIG, so if the
    guard compared against that view, a controller upgrade adding a new
    default (exactly what wakeArbitrationMs was) would make every save from
    an already-open dashboard tab look like a deletion of that key and be
    refused. Comparing against the RAW stored config — what an operator has
    actually persisted — keeps legitimate saves working while still
    catching a genuinely destructive partial write.
    """
    raw_stored = {k: v for k, v in LIVE_CONFIG.items()}   # no new key yet
    stale_dashboard_body = dict(raw_stored)               # lacks the new default
    assert dropped_keys(stale_dashboard_body, raw_stored) == []

    # ...while the destructive partial write is still refused.
    assert len(dropped_keys({"wakeArbitrationMs": 700}, raw_stored)) == 26


def test_guard_reads_raw_stored_config_not_the_underlaid_view():
    """The guard must call the raw accessor, or the false positive returns."""
    src = (CONTROLLER / "em_api.py").read_text()
    m = re.search(r"async def _post_global_config\(.*?(?=\nasync def )", src, re.S)
    assert m
    assert "get_global_device_config_raw" in m.group(0)


# ── decorator-placement guards ────────────────────────────────────────────
#
# Inserting a helper immediately above an already-decorated handler silently
# steals its decorator: on 2026-07-20 `_dropped_keys` was written directly
# under `@auth.require_admin`, so the decorator bound to the helper instead
# and `_post_global_config` was left with NO admin requirement — an auth
# bypass on a config-write endpoint. It surfaced only as a 500 in live
# testing, because the helper was then called with two args while wrapped to
# take a request.
#
# The unit tests above could not catch it: they exec the extracted source
# text, which drops decorators entirely, so they were exercising a different
# function than production. These parse the real file instead.

def _ast_tree():
    import ast
    return ast.parse((CONTROLLER / "em_api.py").read_text())


def _decorators_of(name):
    import ast
    for n in ast.walk(_ast_tree()):
        if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and n.name == name:
            return [getattr(d, "id", getattr(d, "attr", "?")) for d in n.decorator_list]
    raise AssertionError(f"{name} not found in em_api.py")


def test_dropped_keys_is_a_plain_helper_not_a_route_handler():
    assert _decorators_of("_dropped_keys") == [], (
        "_dropped_keys has picked up a decorator — it is a pure helper. This "
        "means it was inserted directly beneath a decorated handler and stole "
        "that decorator."
    )


@pytest.mark.parametrize("handler", [
    "_post_global_config",
    "_post_device_config",
    "_post_upload_binary",
    # The Spotify / AirPlay binaries. Strictly more dangerous than a config
    # write: one stores an arbitrary executable and the other puts it on a
    # device and marks it 755. The reads are here too because their responses
    # name sizes and md5s of programs and sit beside the install they enable.
    "_get_endpoint_binaries",
    "_post_endpoint_binary_upload",
    "_get_device_endpoint_bins",
    "_post_device_endpoint_bin",
])
def test_mutating_handlers_still_require_admin(handler):
    """Any config/binary write must keep its auth decorator."""
    assert "require_admin" in _decorators_of(handler), (
        f"{handler} lost its @auth.require_admin — anyone could call it"
    )


def test_read_endpoint_status_is_a_plain_helper_not_a_route_handler():
    # Same hazard as _dropped_keys above, and the same shape: this helper
    # sits directly above a decorated handler, so if it ever picks up a
    # decorator it has stolen one — leaving a route that installs an
    # executable on a device with no admin requirement.
    assert _decorators_of("_read_endpoint_status") == [], (
        "_read_endpoint_status has picked up a decorator — it is a pure "
        "helper, and taking one means the handler below it lost it."
    )


# ── #325: keys a body cannot contain are not keys it deletes ─────────────────
#
# A device whose ring colour had ever been set could not have its config saved
# from the dashboard again. Every attempt was refused with
#
#   This body would delete 3 existing setting(s): idleEffect, idleRing,
#   idleRingBrightness.
#
# and whatever the user had typed was gone on the next reload. Reported from a
# live dashboard 2026-09-22 by somebody who had entered an AirPlay name several
# times over several days.
#
# The two halves were each correct and composed into a trap. STATE_KEYS are the
# keys a user never sets — em_config_sections says they are "deliberately not
# on a dashboard Stage", and the controller writes them itself from a Home
# Assistant light change and from every volume_state report. And the per-device
# handler put them unconditionally in scope for the DROP check. So the moment
# one was stored it was permanently absent from every future body and
# permanently counted as a deletion.

def _handler_body(src: str, handler: str) -> str:
    """One handler's source, the same way test_both_write_paths_are_guarded
    slices it."""
    m = re.search(rf"async def {handler}\(.*?(?=\nasync def )", src, re.S)
    assert m, f"could not locate {handler}"
    return m.group(0)


def _state_keys():
    """The real set, read from the module, so this cannot drift from it."""
    src = CONTROLLER / "em_config_sections.py"
    ns: dict = {}
    exec(compile(src.read_text(), str(src), "exec"), ns)
    return ns["STATE_KEYS"]


def test_the_dashboard_mirror_of_state_keys_is_complete():
    """The drift that caused #325, pinned.

    The dashboard keeps its own copy of STATE_KEYS and says so in a comment.
    It had `['startupVolume']` while the server had four; the three ring keys
    were added on one side only. effectiveConfig then dropped them, the form
    never carried them, and every per-device save was refused as a body
    deleting three settings the user has no control for.

    Two hand-kept copies of one list is the shape. Comparing them is the only
    thing that can notice.
    """
    dash = (CONTROLLER / "static" / "dashboard.jsx").read_text()
    m = re.search(r"const STATE_KEYS = \[(.*?)\];", dash, re.S)
    assert m, "the dashboard's STATE_KEYS mirror is gone — was it renamed?"
    mirrored = set(re.findall(r"'([^']+)'", m.group(1)))
    assert mirrored == set(_state_keys()), (
        "the dashboard's STATE_KEYS and em_config_sections.STATE_KEYS have "
        f"drifted: dashboard has {sorted(mirrored)}, server has "
        f"{sorted(_state_keys())} — see #325")


def test_a_stored_ring_colour_does_not_block_a_save():
    """The exact shape reported: three state keys stored, none in the body."""
    stored = {"idleEffect": "breathe", "idleRing": "#ff8800",
              "idleRingBrightness": 40, "airplayName": ""}
    body = {"airplayName": "Testgeraet"}
    in_scope = set(stored) | set(body)
    stored_in_scope = {k: v for k, v in stored.items()
                       if k in in_scope and k not in _state_keys()}
    assert dropped_keys(body, stored_in_scope) == [], (
        "a save is refused because of keys the dashboard has no field for")


def test_a_real_user_setting_is_still_caught():
    """The exclusion is narrow: only STATE_KEYS, and only those."""
    stored = {"idleRing": "#ff8800", "owwThreshold": 0.5, "airplayName": "X"}
    body = {"airplayName": "Y"}
    in_scope = set(stored) | set(body)
    stored_in_scope = {k: v for k, v in stored.items()
                       if k in in_scope and k not in _state_keys()}
    assert dropped_keys(body, stored_in_scope) == ["owwThreshold"]


def test_the_device_path_excludes_state_keys_and_the_global_path_does_not():
    """Where the exclusion lives, and where it must not.

    `_post_global_config` writes the body straight through — it really does
    replace — so a state key missing from a fleet body really would be lost
    there. The per-device handler merges (`{**current, **values}`), which is
    what makes the exclusion safe on that side and only that side.
    """
    src = (CONTROLLER / "em_api.py").read_text()
    dev = _handler_body(src, "_post_device_config")
    glob = _handler_body(src, "_post_global_config")
    assert "STATE_KEYS" in dev and "not in sections_mod.STATE_KEYS" in dev, (
        "the per-device drop check no longer excludes STATE_KEYS — #325 is back")
    assert "STATE_KEYS" not in glob, (
        "the global handler has grown a STATE_KEYS exclusion. That path "
        "REPLACES the stored dict, so a state key missing from the body is "
        "really lost there and the guard is the only thing that says so.")
    assert "{**current, **values}" in dev, (
        "the per-device handler no longer merges, so excluding STATE_KEYS "
        "from its drop check would let a save delete them for real")
