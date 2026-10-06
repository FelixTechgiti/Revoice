"""
Every HTTP route is authorised, and the exceptions are named here with a reason.

Two bugs of the same shape, found four weeks apart. Upstream found two GET
routes with no decorator at all — `/api/devices/{id}/oww_assets` (which opens
a shell session on the device and holds the shell lock for up to 120s) and
`/api/releases/controller`. This fork found four more on 2026-09-21, by
reaching `GET /api/devices/{id}/emos` unauthenticated through Home Assistant's
ingress while every neighbouring route answered 401; the worst of them was
`POST /api/devices/{id}/emos_reflash`, which writes a device's boot partition.

Nothing caught either, and nothing could have: a decorator is one line above a
function, and the only thing that notices a missing line is a reader who
already suspects it. Every one of those six had a correctly-decorated sibling
doing the same kind of work three functions away.

So the rule is inverted here. The routes are enumerated from `create_app`, a
route is authorised unless it appears in `PUBLIC` with a reason, and adding a
route to `PUBLIC` is the deliberate act a reviewer can see in a diff.

Read with `ast`, not regexes over the text: a regex finds the words in a
comment explaining them and passes (see source-guards-match-their-own-prose).
"""

import ast
from pathlib import Path

CONTROLLER = Path(__file__).resolve().parents[1]


# Reachable with no session, each for a reason that has to survive being read
# out loud.
PUBLIC = {
    "_serve_spa":
        "is the login page itself",
    "_serve_dashboard":
        "is a static shell — every call it then makes is authenticated",
    "_redirect_root":
        "redirects, and reveals nothing by doing so",
    "_get_setup_state":
        "answers one boolean, whether a bootstrap token is outstanding. The "
        "login page needs it before anyone can sign in",
    "_post_setup":
        "creates the FIRST admin, against the one-time bootstrap token — "
        "there is no session to require because none can exist yet",
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
WS_SELF_AUTH = {
    "_ws_shell": "auth.ws_resolve_session + an explicit admin check",
    "_ws_events": "auth.ws_resolve_session",
}

DECORATORS = {"require_auth", "require_admin"}


def _module() -> ast.Module:
    return ast.parse((CONTROLLER / "em_api.py").read_text())


def _routes(tree) -> dict[str, tuple[str, str]]:
    """handler name -> (method, path), for every app.router.add_<m>(path, fn)."""
    out: dict[str, tuple[str, str]] = {}
    for node in ast.walk(tree):
        if not (isinstance(node, ast.Call)
                and isinstance(node.func, ast.Attribute)
                and node.func.attr.startswith("add_")
                and node.func.attr != "add_static"
                and len(node.args) == 2
                and isinstance(node.args[1], ast.Name)):
            continue
        path = node.args[0].value if isinstance(node.args[0], ast.Constant) else "?"
        out[node.args[1].id] = (node.func.attr[len("add_"):], path)
    return out


def _functions(tree) -> dict[str, ast.AsyncFunctionDef]:
    return {n.name: n for n in tree.body if isinstance(n, ast.AsyncFunctionDef)}


def _decorators(fn) -> set[str]:
    return {d.attr for d in fn.decorator_list if isinstance(d, ast.Attribute)}


def _calls(fn, attr: str) -> bool:
    return any(isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute)
               and n.func.attr == attr for n in ast.walk(fn))


def _compares_role_to_admin(fn) -> bool:
    """`user["role"] != "admin"` — written as a tree so a comment cannot pass."""
    for n in ast.walk(fn):
        if not isinstance(n, ast.Compare):
            continue
        if not isinstance(n.left, ast.Subscript):
            continue
        key = n.left.slice
        if not (isinstance(key, ast.Constant) and key.value == "role"):
            continue
        if any(isinstance(c, ast.Constant) and c.value == "admin"
               for c in n.comparators):
            return True
    return False


def test_the_route_table_was_found():
    """Guards the guard: a refactor of create_app this parser no longer
    understands must fail loudly, not pass over an empty table — the shape of
    'green because it looked at nothing' this file exists to prevent."""
    routes = _routes(_module())
    assert len(routes) > 50
    assert "_get_devices" in routes


def test_every_route_is_authorised_or_named_public():
    tree = _module()
    fns = _functions(tree)
    naked = []
    for name, (_meth, path) in sorted(_routes(tree).items()):
        assert name in fns, f"route {path} -> {name}: handler not found"
        if name in PUBLIC or name in WS_SELF_AUTH:
            continue
        if not (_decorators(fns[name]) & DECORATORS):
            naked.append(f"{path}  ({name})")
    assert not naked, (
        "These routes have no auth.require_* decorator:\n  "
        + "\n  ".join(naked)
        + "\n\nAdd @auth.require_auth or @auth.require_admin, or add the "
          "handler to PUBLIC in this file with the reason it may be reached "
          "without a session."
    )


def test_the_websocket_routes_still_resolve_a_session_themselves():
    """They are exempt from the decorator, never from authorisation. If one
    stops calling ws_resolve_session it becomes the same hole with an
    exemption already written for it."""
    fns = _functions(_module())
    for name in WS_SELF_AUTH:
        assert _calls(fns[name], "ws_resolve_session"), (
            f"{name} is exempt from the decorator because it resolves the "
            f"session itself, and it no longer does"
        )


def test_the_shell_websocket_is_admin():
    fns = _functions(_module())
    assert _compares_role_to_admin(fns["_ws_shell"]), (
        "the shell WebSocket proxies a ROOT shell to a device and must "
        "check for admin"
    )


def test_anything_that_changes_a_device_is_admin():
    """
    A POST to a device is an action on somebody's hardware. `require_auth`
    would let any signed-in non-admin take it, which is the distinction the
    two decorators exist to draw.
    """
    tree = _module()
    fns = _functions(tree)
    weak = []
    for name, (meth, path) in sorted(_routes(tree).items()):
        if meth != "post" or name in PUBLIC:
            continue
        if not path.startswith("/api/devices/"):
            continue
        if "require_admin" not in _decorators(fns[name]):
            weak.append(f"{path}  ({name})")
    assert not weak, (
        "These device-changing routes are not admin-only:\n  "
        + "\n  ".join(weak)
    )


def test_the_reflash_route_is_admin():
    """Named on its own because it writes a BOOT PARTITION, and because it
    is one of the six that shipped open."""
    fns = _functions(_module())
    assert "require_admin" in _decorators(fns["_post_emos_reflash"])


def test_oww_assets_and_controller_release_need_a_session():
    """The other two, named for the same reason."""
    fns = _functions(_module())
    assert _decorators(fns["_get_oww_assets"]) & DECORATORS
    assert _decorators(fns["_get_controller_release"]) & DECORATORS


def test_every_public_exemption_carries_a_reason():
    for name, why in {**PUBLIC, **WS_SELF_AUTH}.items():
        assert why and len(why) > 10, f"{name} is exempt with no reason given"


def test_the_exemptions_are_all_real_routes():
    """An exemption for a handler that no longer exists is a line that reads
    as deliberate and protects nothing — and would silently cover a NEW
    handler that happened to reuse the name."""
    handlers = set(_routes(_module()))
    stale = (set(PUBLIC) | set(WS_SELF_AUTH)) - handlers
    assert not stale, f"exemptions for routes that do not exist: {sorted(stale)}"
