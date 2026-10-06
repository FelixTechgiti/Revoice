"""
No fire-and-forget task may be left unreferenced.

asyncio holds only a WEAK reference to a task — "Save a reference to the
result of this function, to avoid a task disappearing mid-execution", from
`asyncio.create_task`'s own documentation — so a task nothing else holds can
be collected part-way through.

The failure is silent and load-dependent, which is the worst combination
this tree deals with: the work simply does not finish, with no exception and
no log line. The consequences are not uniform either. Losing the wake word
listener's restart leaves a device permanently deaf; losing an OTA leaves it
half-updated; losing a media command leaves HA's entity showing something
the speaker is not doing.

This is an AST guard rather than a grep, because the thing that makes a call
safe is what happens to its RESULT, which no regex can see.
"""

import ast
from pathlib import Path

CONTROLLER = Path(__file__).resolve().parents[1]

# Every module that starts background work. Listed rather than globbed: a
# new module that spawns tasks should have to be added here deliberately,
# which is the moment to think about who holds its references.
MODULES = (
    "em_controller.py", "em_api.py", "em_esphome.py",
    "em_player.py", "em_ns.py", "em_tap_burst.py",
)


def _unreferenced_create_tasks(path: Path):
    """
    Lines where `create_task(...)` is a bare expression statement — the
    result assigned to nothing, chained to nothing, awaited by nothing.

    A call whose result is assigned, returned, awaited, or has a
    done-callback chained onto it is fine: something holds it.
    """
    tree = ast.parse(path.read_text())
    for parent in ast.walk(tree):
        for child in ast.iter_child_nodes(parent):
            child.parent = parent

    out = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        name = func.attr if isinstance(func, ast.Attribute) else getattr(func, "id", None)
        if name != "create_task":
            continue
        if isinstance(node.parent, ast.Expr):
            out.append(node.lineno)
    return out


def test_no_task_is_started_without_something_holding_it():
    strays = {
        module: lines
        for module in MODULES
        if (lines := _unreferenced_create_tasks(CONTROLLER / module))
    }
    assert not strays, (
        "these tasks can be garbage-collected mid-execution — start them "
        f"through the module's _spawn helper instead: {strays}"
    )


def test_the_spawn_helper_holds_a_reference_and_reports_failures():
    """
    Holding the task is half of it. A task nobody awaits also swallows its
    exception until some later collection surfaces it as "Task exception
    was never retrieved", by which point the context is gone.

    One helper for the whole controller since the 2026-09-28 upstream sync:
    em_tasks.spawn. It replaced a per-module `_spawn` in em_api and
    em_controller and a per-connection one on the ESPHome satellite, all
    three of which did exactly this and none of which were reachable from
    the others.
    """
    src = (CONTROLLER / "em_tasks.py").read_text()
    body = src[src.index("def spawn("):]
    assert "_live.add" in body, "em_tasks.spawn must hold the task"
    assert "add_done_callback" in body, "and release it again, or the set is a leak"
    finished = src[src.index("def _finished("):]
    assert "_live.discard" in finished, "the set must be released, or it leaks"
    assert "exception" in finished and "log.error" in finished, \
        "and a failure must be reported where it happens"


def test_nothing_starts_a_task_outside_that_helper():
    """
    The helper only helps where it is used. Each module that starts
    background work goes through em_tasks, so there is one place that can be
    got wrong rather than four.
    """
    strays = {}
    for module in MODULES:
        src = (CONTROLLER / module).read_text()
        if "em_tasks" not in src:
            continue
        for name in ("_spawn(", "_background_tasks"):
            if name in src:
                strays.setdefault(module, []).append(name)
    assert not strays, (
        "these modules kept a second spawn helper beside em_tasks — one of "
        f"them will be the one somebody forgets: {strays}")
