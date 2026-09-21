"""
Every /api route is authorised, and the exceptions are named here.

**Four routes shipped with no authorisation at all**, found 2026-09-21 by
reaching `GET /api/devices/{id}/emos` unauthenticated through Home
Assistant's ingress while every neighbouring route answered 401. The worst
of them was `POST /api/devices/{id}/emos_reflash`, which writes a device's
boot partition.

Nothing caught it, and nothing could have: a decorator is one line above a
function, and the only thing that notices a missing line is a reader who
already suspects it. Every one of those four had a correctly-decorated
sibling doing the same kind of work three functions away.

So the rule is inverted here. A route is authorised unless it appears in
`PUBLIC` with a reason, and adding a route to `PUBLIC` is the deliberate act
that a reviewer can see in a diff.
"""

import re
from pathlib import Path

CONTROLLER = Path(__file__).resolve().parents[1]
SRC = (CONTROLLER / "em_api.py").read_text()


# Routes that are reachable with no session, each for a reason that has to
# survive being read out loud.
PUBLIC = {
    "_post_setup":
        "creates the FIRST admin, against the bootstrap token — there is no "
        "session to require because none can exist yet",
    "_get_setup_state":
        "answers one boolean, whether a bootstrap token is outstanding. The "
        "login page needs it before anyone can sign in",
    "_post_login":
        "is how a session is obtained",
    "_post_ingress_login":
        "is the same for a Home Assistant ingress session — em_ingressauth "
        "validates what Supervisor forwarded",
    "_post_logout":
        "destroys a session; refusing an unauthenticated caller would leave "
        "somebody holding a cookie they cannot drop",
}

# WebSocket routes resolve the session INSIDE the handler, because the
# decorator's token extraction does not read the query string and a browser
# WebSocket cannot set headers. `_ws_shell` says so in its own comment.
WS_INTERNAL = {
    "_ws_shell": "auth.ws_resolve_session + an explicit admin check",
    "_ws_events": "auth.ws_resolve_session",
}


def _routes():
    return re.findall(
        r'add_(get|post|delete|put)\(\s*"(/api/[^"]*)",\s*(\w+)', SRC)


def _decorators(fn: str) -> str:
    m = re.search(rf'((?:^@[^\n]*\n)*)^async def {re.escape(fn)}\(', SRC, re.M)
    return m.group(1) if m else ""


def _body(fn: str) -> str:
    m = re.search(rf'^async def {re.escape(fn)}\(.*?(?=\n(?:@|async def |def )\S|\Z)',
                  SRC, re.M | re.S)
    return m.group(0) if m else ""


def test_there_are_routes_to_check():
    """A regex that silently matches nothing would make every assertion below
    pass over an empty set — the shape of 'green because it looked at
    nothing' this file exists to prevent."""
    assert len(_routes()) > 40


def test_every_api_route_is_authorised():
    naked = []
    for _meth, path, fn in _routes():
        if fn in PUBLIC or fn in WS_INTERNAL:
            continue
        if "auth.require" not in _decorators(fn):
            naked.append(f"{path}  ({fn})")
    assert not naked, (
        "These /api routes have no auth.require_* decorator:\n  "
        + "\n  ".join(sorted(set(naked)))
        + "\n\nAdd @auth.require_auth or @auth.require_admin, or add the "
          "handler to PUBLIC in this file with the reason it may be reached "
          "without a session."
    )


def test_the_websocket_routes_still_resolve_a_session_themselves():
    """They are exempt from the decorator, never from authorisation. If one
    stops calling ws_resolve_session it becomes the same hole with an
    exemption already written for it."""
    for fn in WS_INTERNAL:
        assert "auth.ws_resolve_session" in _body(fn), (
            f"{fn} is exempt from the decorator because it resolves the "
            f"session itself, and it no longer does"
        )


def test_the_shell_websocket_is_admin():
    assert 'user["role"] != "admin"' in _body("_ws_shell"), (
        "the shell WebSocket proxies a ROOT shell to a device and must "
        "check for admin"
    )


def test_anything_that_changes_a_device_is_admin():
    """
    A POST to a device is an action on somebody's hardware. `require_auth`
    would let any signed-in non-admin take it, which is the distinction the
    two decorators exist to draw.
    """
    weak = []
    for meth, path, fn in _routes():
        if meth != "post" or fn in PUBLIC:
            continue
        if not path.startswith("/api/devices/"):
            continue
        if "auth.require_admin" not in _decorators(fn):
            weak.append(f"{path}  ({fn})")
    assert not weak, (
        "These device-changing routes are not admin-only:\n  "
        + "\n  ".join(sorted(set(weak)))
    )


def test_the_reflash_route_is_admin():
    """Named on its own because it writes a BOOT PARTITION, and because it
    is the one that shipped open."""
    assert "auth.require_admin" in _decorators("_post_emos_reflash")


def test_every_public_exemption_carries_a_reason():
    for fn, why in {**PUBLIC, **WS_INTERNAL}.items():
        assert why and len(why) > 10, f"{fn} is exempt with no reason given"


def test_the_exemptions_are_all_real_routes():
    """An exemption for a handler that no longer exists is a line that reads
    as deliberate and protects nothing — and would silently cover a NEW
    handler that happened to reuse the name."""
    handlers = {fn for _m, _p, fn in _routes()}
    stale = (set(PUBLIC) | set(WS_INTERNAL)) - handlers
    assert not stale, f"exemptions for routes that do not exist: {sorted(stale)}"
