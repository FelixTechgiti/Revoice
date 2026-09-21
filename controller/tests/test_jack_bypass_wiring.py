"""
The bass-guard-on-jack bypass (#231), and the seven places it is wired.

The setting is unusual in this tree: it rides the config push like every other
playback key, and the CONTROLLER never reads it. Its whole implementation is on
the device, because resolving it needs the plug position and no device reports
that to the controller. So the ordinary mirror guards
(tests/test_config_mirrors.py) cannot cover it, and the things that would break
it are all silent:

  - the key reaching the device but nothing passing it into the chain,
  - the chain never being re-resolved when a plug moves,
  - the dashboard offering it on firmware that cannot act on it,
  - somebody later "finishing" it by adding a controller-side mirror, which is
    the two-limiters-in-series mistake in a new costume.

The device half is pinned on the device side (internal/outchain/chain_test.go
for the resolution, internal/bindings/speaker/outputchain_guard_test.go for the
wiring that cannot be compiled on a host). This file pins the seams between the
two halves.
"""

import re
from pathlib import Path

import em_config_sections as cs
import em_db

ROOT = Path(__file__).resolve().parents[2]
CONTROLLER = ROOT / "controller"
DEVICE = ROOT / "device"

API = (CONTROLLER / "em_api.py").read_text()
CTRL = (CONTROLLER / "em_controller.py").read_text()
DASHBOARD = (CONTROLLER / "static" / "dashboard.jsx").read_text()
CONFIG_GO = (DEVICE / "internal" / "config" / "config.go").read_text()
CONTROL_GO = (DEVICE / "internal" / "client" / "control.go").read_text()
SERVER_GO = (DEVICE / "cmd" / "server.go").read_text()

KEY = "bassGuardJackBypass"
CAP = "jack_detect"


# ─── The setting itself ──────────────────────────────────────────────────────

def test_the_default_is_off():
    """
    The caveat on #231 is the whole reason this is a setting at all:
    `Ext_Speaker_Amp_Switch` was once observed Off while the internal speaker
    was audibly playing, and that has not been retested. If it does not gate
    the internal driver, bypassing the guard with a plug in feeds unguarded
    bass to a 1.5" driver — so the default may not become True until somebody
    has confirmed on hardware that the driver really is out of the path.
    """
    assert em_db.DEFAULT_DEVICE_CONFIG[KEY] is False


def test_it_is_a_playback_setting():
    assert KEY in cs.SECTIONS["playback"]["keys"]
    assert KEY not in cs.STATE_KEYS


# ─── The controller must NOT act on it ───────────────────────────────────────

def test_no_controller_side_consumer():
    """
    The controller shapes audio for firmware that does not announce
    `output_chain`, and it is never told whether a plug is in — so it CANNOT
    honour this key, and a mirror that pretended to would apply the bypass on
    the wrong devices at the wrong times.

    em_api._apply_live_config and the registration block in em_controller are
    where such a mirror would appear. Both are checked, because either alone
    would leave the other free to grow one.
    """
    live = API.split("async def _apply_live_config")[1].split("\n@auth")[0]
    assert KEY not in live, (
        f"{KEY} has appeared in _apply_live_config — the controller cannot "
        f"resolve it (it never learns the plug position), so a mirror here "
        f"applies the bypass on devices that are not plugged into anything"
    )
    reg = CTRL.split('await device.send_control({"type": "config", **config})')[1][:2500]
    assert KEY not in reg, (
        f"{KEY} has appeared in the registration mirror — see above"
    )


def test_the_shaping_modules_never_learned_about_it():
    """
    em_eq/em_mbc/em_limiter are the controller's own chain. A bypass there
    would be applied from a plug position the controller does not have.
    """
    for name in ("em_eq.py", "em_mbc.py", "em_limiter.py", "em_outchain.py"):
        assert KEY not in (CONTROLLER / name).read_text(), (
            f"{name} reads {KEY} — the controller-side chain cannot know "
            f"whether anything is plugged in"
        )


# ─── The device half, at the seams ───────────────────────────────────────────

def test_the_key_rides_the_config_push_as_a_pointer():
    """
    False is both the default and a value that has to be sendable, so a plain
    bool with `omitempty` would make turning it off indistinguishable from not
    sending it — the same trap `consolePassword` and `duckDb` avoid.
    """
    m = re.search(rf'BassGuardJackBypass\s+\*bool\s+`json:"{KEY},omitempty"`',
                  CONFIG_GO)
    assert m, f"ConfigMessage has no *bool field tagged {KEY}"
    assert "if msg.BassGuardJackBypass != nil {" in CONFIG_GO, (
        "config.Apply does not read the field, so the push is discarded"
    )


def test_the_device_passes_it_into_the_output_chain():
    """
    The one call site that builds outchain.Params. Without this line the key
    arrives, is stored, and changes nothing — with the dashboard reporting a
    saved setting the whole time.
    """
    block = SERVER_GO.split("pcmSpeaker.SetOutputChain(outchain.Params{")[1]
    block = block.split("})")[0]
    assert "GuardBypassOnJack:" in block and "c.BassGuardJackBypass" in block


def test_the_capability_is_announced_only_where_the_jack_can_be_read():
    """
    Whether a jack detect switch EXISTS is a property of the board, so the
    capability is conditional like `ambient_light` — `jack.Inserted()` answers
    "not inserted" for absent hardware, which is indistinguishable from a
    plug being out.
    """
    assert re.search(r'if jack\.Present\(\)\s*\{\s*caps = append\(caps, "jack_detect"\)',
                     CONTROL_GO), (
        "jack_detect is not announced conditionally on jack.Present() — a "
        "board with no jack would claim it and get the setting offered"
    )


# ─── The dashboard offers it only where both halves exist ────────────────────

def test_the_control_needs_the_chain_AND_the_jack():
    """
    Knowing the plug position is useless without running the chain, and
    running the chain is useless without knowing the plug position. The
    controller can supply neither half, so firmware missing either gets the
    toggle DISABLED WITH THE REASON rather than a control that saves a key
    nothing reads.
    """
    assert '"outputChainCapable"' in API and '"jackDetectCapable"' in API, (
        "em_api does not report both capabilities, so the dashboard cannot "
        "tell the two reasons apart"
    )
    assert 'return "jack_detect" in (self.capabilities or [])' in CTRL

    gate = re.search(
        r"jackBypassCapable=\{[^}]*outputChainCapable[^}]*jackDetectCapable[^}]*\}",
        DASHBOARD, re.S)
    assert gate, "the dashboard gate does not require BOTH capabilities"

    toggle = re.search(r"<Toggle label=\{t\('cfgJackBypass'\)\}.*?/>",
                       DASHBOARD, re.S)
    assert toggle, "the jack-bypass toggle is gone"
    assert "disabled={!jackBypassCapable}" in toggle.group(0)
    assert "t('cfgNoJackBypass')" in toggle.group(0), (
        "a disabled control must say WHY — an inert toggle with no reason "
        "reads as a broken feature"
    )
