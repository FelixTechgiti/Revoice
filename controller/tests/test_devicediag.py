"""
The probe that answers why a local endpoint is not working.

Every case here is a pair that used to be indistinguishable from the
controller, and each pair wants a different next move. The evening this came
from (2026-09-20) had exactly one symptom for four different causes: Spotify
Connect restarting once a minute.
"""

import em_devicediag as diag


def _answer(**over):
    base = {
        "DNSSOCK": "yes",
        "GAI": "222 ",
        "PING": "PING apresolve.spotify.com (35.186.224.47) 56(84) bytes of data.",
        "PORTS": "0.0.0.0:5000 :::7000 0.0.0.0:36000",
        "AP2": "yes",
        "CLASSIC": "yes",
        "NQPTP": "yes",
        "NQPTPRUN": "1",
    }
    base.update(over)
    body = "\n".join(f"{k}:{v}" for k, v in base.items())
    return body + "\n" + diag.DIAG_MARK


# ── The command ──────────────────────────────────────────────────────────────

def test_the_resolver_probe_is_a_bionic_binary():
    """busybox here is static and carries its own resolver reading
    /etc/resolv.conf — a file no endpoint's libc has ever opened. A probe
    through it answers for the wrong resolver and would have said DNS was
    fine on the very device where nothing could resolve."""
    cmd = diag.diag_cmd()
    assert "/system/bin/ping" in cmd
    assert "busybox ping" not in cmd
    assert "busybox nslookup" not in cmd


def test_the_probe_asks_for_the_name_that_actually_failed():
    assert diag.DNS_PROBE_HOST in diag.diag_cmd()


def test_the_path_the_endpoints_use_is_probed_directly():
    """bionic has two resolver paths and `ping` takes the OTHER one.

    emOS answered only `getaddrinfo` until 0.7.0-fx.1, so a ping-only probe
    reported a broken resolver on a device whose resolver worked — measured
    2026-09-21. There is no binary on the device known to take the endpoints'
    path, so the conversation is held directly, in the words bionic uses.
    """
    cmd = diag.diag_cmd()
    assert "nc -U /dev/socket/dnsproxyd" in cmd
    assert "getaddrinfo " in cmd


def test_the_probe_is_read_only():
    """It runs on somebody's only-reachable-over-the-network device while
    they are asleep. Stderr redirections are allowed and are the only `>`
    that may appear — anything else is a write."""
    cmd = diag.diag_cmd()
    for verb in (" rm ", " mv ", " dd ", "kill", "reboot", "chmod"):
        assert verb not in cmd, verb
    writes = cmd.replace("2>&1", "").replace("2>/dev/null", "")
    assert ">" not in writes


# ── Resolution ───────────────────────────────────────────────────────────────

def test_a_resolved_name_is_ok_even_when_every_packet_is_lost():
    """The address in the first line IS the resolution. A router that drops
    ICMP would otherwise read as a device that cannot resolve — which is the
    fault being hunted, reported about a working resolver."""
    out = _answer(GAI="", PING="PING apresolve.spotify.com (35.186.224.47) "
                              "56(84) bytes of data.")
    assert diag.parse_diag(out)["dns"] == "ok"


def test_an_unresolvable_name_with_a_socket_is_the_resolver_not_the_network():
    # GAI empty: no `nc -U`, so ping is what is left — the case this test is
    # about.
    out = _answer(GAI="", PING="ping: unknown host apresolve.spotify.com")
    d = diag.parse_diag(out)
    assert d["dns"] == "unresolved"
    assert diag.summary(d) == "dns_unresolved"


def test_an_unresolvable_name_with_no_socket_names_the_missing_proxy():
    """The emOS half is simply not answering — an init below 0.6.0-fx.1, or
    one that could not bind. Different repair entirely."""
    out = _answer(DNSSOCK="no", GAI="",
                  PING="ping: bad address 'apresolve.spotify.com'")
    d = diag.parse_diag(out)
    assert d["dns"] == "no_socket"
    assert diag.summary(d) == "dns_no_socket"


def test_a_missing_probe_binary_is_not_a_verdict_about_the_device():
    out = _answer(GAI="", PING="sh: /system/bin/ping: not found")
    d = diag.parse_diag(out)
    assert d["dns"] == "no_tool"
    assert diag.summary(d) == "dns_unknown"


def test_silence_from_the_probe_is_unknown():
    assert diag.parse_diag(_answer(GAI="", PING=""))["dns"] == "unknown"


# ── Ports and binaries ───────────────────────────────────────────────────────

def test_ports_are_read_off_the_local_address_whatever_the_family():
    d = diag.parse_diag(_answer(PORTS="0.0.0.0:5000 :::7000 127.0.0.1:36000"))
    assert d["ports"] == [5000, 7000, 36000]
    assert d["airplayListening"] and d["airplay2Listening"] and d["spotifyListening"]


def test_nothing_listening_is_reported_as_silent_endpoints():
    d = diag.parse_diag(_answer(PORTS=""))
    assert d["ports"] == []
    assert diag.summary(d) == "endpoints_silent"


def _wanted(out):
    """The device's answer with AirPlay 2 switched on, which is the only
    state in which its parts are worth reporting on."""
    return diag.with_intent(diag.parse_diag(out), airplay_on=True,
                            airplay2_on=True, spotify_on=True)


def test_airplay2_without_its_clock_is_named_rather_than_left_to_guess():
    d = _wanted(_answer(NQPTP="no", NQPTPRUN="0"))
    assert d["ap2Installed"] and not d["nqptpInstalled"]
    assert diag.summary(d) == "ap2_without_clock"


def test_an_installed_clock_that_is_not_running_is_its_own_answer():
    d = _wanted(_answer(NQPTPRUN="0"))
    assert d["nqptpRunning"] is False
    assert diag.summary(d) == "ap2_clock_not_running"


def test_a_ps_that_did_not_run_is_unknown_rather_than_stopped():
    """`grep -c` answers 0 for "not running" and for "ps is not there"; only
    the second must read as unknown, or a working clock daemon gets reported
    as stopped on a device whose ps is missing."""
    d = diag.parse_diag(_answer(NQPTPRUN=""))
    assert d["nqptpRunning"] is None
    assert diag.summary(d) == "ok"


# ── Could not ask ────────────────────────────────────────────────────────────

def test_an_unmarked_answer_is_not_a_device_with_nothing_listening():
    assert diag.parse_diag("DNSSOCK:no\nPORTS:") is None
    assert diag.parse_diag("") is None
    assert diag.summary(None) == "not_asked"


def test_a_healthy_device_says_so():
    assert diag.summary(diag.parse_diag(_answer())) == "ok"


# ── The wiring ───────────────────────────────────────────────────────────────

def test_the_endpoint_asks_both_questions_in_one_shell_session():
    """Two round trips would double a wait that is already ~26s, and both
    answers are wanted by the same person at the same moment."""
    import sys
    from pathlib import Path
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import apisrc
    src = apisrc.extract("_get_device_emos")
    assert "em_devicediag.diag_cmd()" in src
    assert src.count("_shell_run(") == 1
    # Present on every answer, including the ones that return before any
    # shell runs.
    assert '"diagSummary": "not_asked"' in src


def test_the_panel_renders_the_servers_verdict_rather_than_its_own():
    jsx = (__import__("pathlib").Path(__file__).resolve().parents[1]
           / "static" / "dashboard.jsx").read_text()
    assert "diag_' + emos.diagSummary" in jsx
    # Every verdict the server can produce needs a string, in both languages.
    strings = (__import__("pathlib").Path(__file__).resolve().parents[1]
               / "static" / "strings.js").read_text()
    for key in ("ok", "not_asked", "dns_no_socket", "dns_unresolved",
                "dns_unknown", "endpoints_silent", "ap2_not_installed",
                "ap2_without_clock", "ap2_clock_not_running"):
        assert strings.count(f"diag_{key}:") == 2, key
    for key in ("ok", "no_socket", "unresolved", "no_tool", "unknown"):
        assert strings.count(f"diagDns_{key}:") == 2, key


# ── Intent ───────────────────────────────────────────────────────────────────

def test_an_endpoint_nobody_asked_for_is_not_a_fault():
    """Reporting a device that is doing exactly what it was told to do is how
    a diagnosis line becomes one people learn to ignore."""
    d = diag.with_intent(diag.parse_diag(_answer(PORTS="", NQPTP="no")),
                         airplay_on=False, airplay2_on=False,
                         spotify_on=False)
    assert diag.summary(d) == "ok"


def test_airplay2_switched_on_without_its_binary_is_named():
    d = diag.with_intent(diag.parse_diag(_answer(AP2="no")),
                         airplay_on=True, airplay2_on=True, spotify_on=True)
    assert diag.summary(d) == "ap2_not_installed"


def test_classic_airplay_is_not_accused_of_missing_airplay2_parts():
    d = diag.with_intent(diag.parse_diag(_answer(AP2="no", NQPTP="no")),
                         airplay_on=True, airplay2_on=False, spotify_on=True)
    assert diag.summary(d) == "ok"


def test_a_switched_on_endpoint_that_is_not_listening_is_silent():
    d = diag.with_intent(diag.parse_diag(_answer(PORTS="0.0.0.0:36000")),
                         airplay_on=True, airplay2_on=False, spotify_on=True)
    assert diag.summary(d) == "endpoints_silent"


def test_intent_on_a_probe_that_did_not_run_stays_unasked():
    assert diag.with_intent(None, airplay_on=True, airplay2_on=True,
                            spotify_on=True) is None


def test_the_inits_own_log_rides_along():
    """A proxy that could not bind says so in /run/net.log and nowhere else —
    a socket that was never created is indistinguishable from an init too old
    to create one, from the outside."""
    out = _answer(NETLOG="[  12345] wlan0 up|[  12900] dnsproxyd: could not "
                         "bind /dev/socket/dnsproxyd: No such file|")
    d = diag.parse_diag(out)
    assert len(d["netlog"]) == 2
    assert "could not bind" in d["netlog"][1]
    # A device without one says nothing rather than an empty-looking line.
    assert diag.parse_diag(_answer(NETLOG=""))["netlog"] == []
    # It survives the intent fold, which copies the dict.
    folded = diag.with_intent(d, airplay_on=True, airplay2_on=True,
                              spotify_on=True)
    assert folded["netlog"] == d["netlog"]


def test_the_log_tail_is_bounded():
    """It rides a shell round trip somebody is waiting on, and a 128KB file
    behind it."""
    assert "tail -n 6" in diag.diag_cmd()


# ── The two resolver paths ───────────────────────────────────────────────────

def test_the_endpoints_path_decides_when_it_can_be_measured():
    # It answered: ping's verdict does not matter, whichever way it went.
    d = diag.parse_diag(_answer(GAI="222 ", PING="ping: unknown host x"))
    assert d["dns"] == "ok"
    assert d["dnsPath"] == "getaddrinfo"


def test_a_refusal_on_that_path_is_the_resolver():
    d = diag.parse_diag(_answer(GAI="501 ", PING="ping: unknown host x"))
    assert d["dns"] == "unresolved"
    d = diag.parse_diag(_answer(DNSSOCK="no", GAI="501 "))
    assert d["dns"] == "no_socket"


def test_an_unmeasurable_path_falls_back_and_says_so():
    """No `nc -U` on this busybox: the reading is ping's, and the panel has to
    name that, because on an old emOS ping fails whatever the resolver does."""
    for raw in ("", "nc: unrecognized option -U", "   "):
        d = diag.parse_diag(_answer(GAI=raw, PING="ping: unknown host x"))
        assert d["dnsPath"] == "gethostbyname", raw
        assert d["dns"] == "unresolved", raw


def test_airplay2_is_not_judged_on_a_port_this_firmware_does_not_use():
    """shairport-sync's AP2 build defaults to 7000; this firmware pins both
    flavours to 5000. Judging 7000 would accuse every working device."""
    d = diag.with_intent(diag.parse_diag(_answer(PORTS="0.0.0.0:5000 0.0.0.0:36000")),
                         airplay_on=True, airplay2_on=True, spotify_on=True)
    assert d["airplay2Listening"] is False
    assert diag.summary(d) == "ok"
