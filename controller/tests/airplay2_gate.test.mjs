// Tests for airplay2Gate() in dashboard.jsx — whether the AirPlay 2 switch
// can be used, and what is said under it.
//
//     node controller/tests/airplay2_gate.test.mjs
//
// Source extraction rather than import, for endpoint_health.test.mjs's
// reason: the dashboard compiles to one classic script with no module
// boundary, so the alternative is a second copy that drifts.
//
// **This exists because the switch shipped unusable.** The controller
// installs the AirPlay 2 receiver only for a device whose `airplay2Enabled`
// is on — every kind is gated on its own toggle, so a lossy link is never
// spent on a program nobody asked for. The first version of this gate then
// disabled the toggle until that binary was present, which closed the loop:
// no file until the switch is on, no switch until the file is there. The
// sub-label said the receiver would "arrive with the next endpoint update",
// which it never could.
//
// Nothing failed. Every test passed, CI was green, the release went out, and
// the fault was found by somebody looking for the switch. A gate written as
// an inline JSX expression cannot be driven by anything, which is why it is a
// function now: the property worth pinning is not what it renders, it is that
// there EXISTS a reachable path to the setting being on.

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import assert from "assert";

const HERE = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(HERE, "..", "static", "dashboard.jsx"), "utf8");

// Like endpoint_health.test.mjs's lifter, with one difference that matters:
// the body brace is found AFTER the parameter list closes. This function
// destructures its argument, so the first `{` in the source belongs to the
// parameters, and matching from there lifts the signature and nothing else.
function liftFunction(name) {
  const start = src.indexOf(`function ${name}`);
  if (start < 0) {
    throw new Error(`dashboard.jsx no longer defines ${name}() — if it was `
                  + `renamed or moved, update this test to match`);
  }
  let i = src.indexOf("(", start);
  let parens = 0;
  for (; i < src.length; i++) {
    if (src[i] === "(") parens++;
    else if (src[i] === ")") { parens--; if (parens === 0) { i++; break; } }
  }
  let depth = 0;
  i = src.indexOf("{", i);
  for (; i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") { depth--; if (depth === 0) break; }
  }
  return src.slice(start, i + 1);
}

const airplay2Gate = new Function(
  `${liftFunction("airplay2Gate")}; return airplay2Gate;`)();

// A device that can do it, on emOS, with AirPlay on and nothing installed.
const READY = {
  airplayCapable: true, airplay2Capable: true, airplayOn: true,
  ap2Installed: false, baseOs: "emos", stored: false,
};

// ── The one that was broken ──────────────────────────────────────────────

{
  const g = airplay2Gate(READY);
  assert.strictEqual(g.disabled, false,
    "THE DEADLOCK: the binary arrives BECAUSE this is turned on, so requiring "
    + "it first makes the setting unreachable for ever");
  assert.strictEqual(g.reason, "cfgAirplay2Coming",
    "it should say the receiver is on its way, not that it is missing");
}

{
  // And once it has landed, the ordinary description.
  const g = airplay2Gate({ ...READY, ap2Installed: true });
  assert.strictEqual(g.disabled, false);
  assert.strictEqual(g.reason, "cfgAirplay2Sub");
}

// ── The switch shows what is STORED, never the install state ─────────────
//
// Masking it would make the control disagree with the setting the instant
// somebody turns it on — the same class of lie as a control that silently
// does nothing, and it would read as the click not having registered.

for (const ap2Installed of [false, true]) {
  const g = airplay2Gate({ ...READY, ap2Installed, stored: true });
  assert.strictEqual(g.value, true,
    `a stored ON must show as on (installed=${ap2Installed})`);
}

// ── The three refusals, each of which a user cannot fix by waiting ───────

{
  const g = airplay2Gate({ ...READY, airplay2Capable: false });
  assert.strictEqual(g.disabled, true);
  assert.strictEqual(g.reason, "cfgNoAirplay2");
  assert.strictEqual(g.value, false,
    "firmware that cannot select a receiver must not show this as on");
}

{
  // FireOS binds two TCP ports per session that the kernel picks at runtime,
  // and drops everything it was not told about in advance (#107). The session
  // negotiates and then plays nothing, which reads as a broken speaker.
  const g = airplay2Gate({ ...READY, baseOs: "fireos" });
  assert.strictEqual(g.disabled, true);
  assert.strictEqual(g.reason, "cfgAirplay2FireOS");
}

{
  const g = airplay2Gate({ ...READY, airplayOn: false });
  assert.strictEqual(g.disabled, true);
  assert.strictEqual(g.reason, "cfgAirplay2NeedsAirplay");
}

// Absence of base_os is NOT FireOS. Firmware too old to report it is the same
// firmware that lacks the capability, so it is already refused above — and a
// warning about a platform nobody confirmed would be a guess on screen.
for (const baseOs of [null, undefined, "emos"]) {
  assert.strictEqual(airplay2Gate({ ...READY, baseOs }).disabled, false,
    `base_os ${String(baseOs)} must not be treated as FireOS`);
}

// ── Every reason it can give has a string in both languages ──────────────
//
// A gate that returns a key nothing translates renders an empty sub-label,
// which is how a refusal becomes a switch that is off for no stated reason.

const strings = readFileSync(join(HERE, "..", "static", "strings.js"), "utf8");
const REASONS = ["cfgAirplay2Sub", "cfgAirplay2Coming", "cfgNoAirplay2",
                 "cfgAirplay2FireOS", "cfgAirplay2NeedsAirplay"];
for (const key of REASONS) {
  const n = (strings.match(new RegExp(`^\\s*${key}:`, "gm")) || []).length;
  assert.strictEqual(n, 2,
    `${key} should be defined once per language in strings.js, found ${n}`);
}

// ── What the Status tab says about AirPlay 2 ─────────────────────────────
//
// Reported by the device since v2.49.0-fx.1 and displayed by nothing, which
// is how "is AirPlay 2 actually on?" came to have no answer on screen.

const airplay2Line = new Function(
  `${liftFunction("airplay2Line")}; return airplay2Line;`)();

const HEALTH_OK   = { enabled: true, alive: true,  restarts: 0 };
const HEALTH_DOWN = { enabled: true, alive: false, restarts: 7 };
const AP2 = { flavour: "airplay2", nqptp: { ok: true } };

{
  // Firmware too old to name the flavour says nothing rather than guessing.
  assert.strictEqual(airplay2Line(null, null, true), null);
  assert.strictEqual(airplay2Line({ ok: true, size: 9 }, null, true), null);
}

{
  const g = airplay2Line({ flavour: "classic" }, null, true);
  assert.deepStrictEqual(g, { flavour: "classic", clock: null, restarts: 0,
                              receiverRunning: null });
}

{
  const g = airplay2Line(AP2, HEALTH_OK, true);
  assert.strictEqual(g.clock, "ok");
}

{
  // The state the whole thing exists for: the receiver runs, the clock does
  // not, classic AirPlay is unaffected and AirPlay 2 plays out of sync — a
  // fault with nothing audible about it.
  const g = airplay2Line(AP2, HEALTH_DOWN, true);
  assert.strictEqual(g.clock, "down");
  assert.strictEqual(g.restarts, 7, "the restart count is the evidence");
}

{
  // The daemon's FILE is missing: settled from the register message, without
  // waiting for a stats tick that would only ever say "not enabled".
  const g = airplay2Line({ flavour: "airplay2", nqptp: { ok: false } },
                         null, true);
  assert.strictEqual(g.clock, "absent");
}

{
  // Installed, but this firmware cannot report liveness, or has not ticked
  // yet. Unknown is its own answer and must not read as the failure above.
  assert.strictEqual(airplay2Line(AP2, null, false).clock, "unknown");
  assert.strictEqual(airplay2Line(AP2, null, true).clock, "unknown");
  assert.strictEqual(airplay2Line(AP2, { enabled: false }, true).clock, "absent");
}

// ── The RECEIVER, which every clock verdict silently presumes is running ──
//
// Each clock string is a claim about the sound: "audio will not
// synchronise" only means anything if there is audio. On a real panel
// (2026-09-19) both endpoint lines were red for ONE cause — neither binary
// could resolve `localhost` — and the flavour line described the clock as a
// separate fault that would spoil playback nobody was getting.
//
// So the receiver's own liveness rides the result, and the renderer chooses
// its wording from it. Three values, because "cannot tell" must not suppress
// a real clock finding.

{
  const g = airplay2Line(AP2, HEALTH_DOWN, true, HEALTH_DOWN);
  assert.strictEqual(g.receiverRunning, false,
    "a receiver reported enabled-and-not-alive is positively down");
  assert.strictEqual(g.clock, "down",
    "the clock verdict is still computed — the renderer decides what to say");
}

{
  const g = airplay2Line(AP2, HEALTH_DOWN, true, HEALTH_OK);
  assert.strictEqual(g.receiverRunning, true,
    "a running receiver is what makes the clock verdict worth printing");
}

{
  // The three ways of not knowing, and all of them leave the verdict alone.
  assert.strictEqual(airplay2Line(AP2, HEALTH_DOWN, true).receiverRunning, null,
    "no receiver health at all is not evidence the receiver is down");
  assert.strictEqual(
    airplay2Line(AP2, HEALTH_DOWN, false, HEALTH_DOWN).receiverRunning, null,
    "firmware that cannot report liveness says nothing about the receiver");
  assert.strictEqual(
    airplay2Line(AP2, HEALTH_DOWN, true, { enabled: false }).receiverRunning, null,
    "an endpoint nobody switched on is not a receiver that failed");
}

{
  // It rides every branch, including the early ones — a classic receiver
  // that is down is the same sentence about a different binary.
  assert.strictEqual(
    airplay2Line({ flavour: "classic" }, null, true, HEALTH_DOWN).receiverRunning,
    false);
  assert.strictEqual(
    airplay2Line({ flavour: "airplay2", nqptp: { ok: false } }, null, true,
                 HEALTH_DOWN).receiverRunning,
    false);
}

{
  // The renderer's own rule, read out of the source: a positively-down
  // receiver must be answered with the receiver strings and never with one
  // of the clock ones. Checked as a SOURCE guard because the ternary chain
  // is in JSX this file cannot lift, and the order of its arms is the whole
  // fix — a later edit that moves the clock arms in front restores the bug
  // with every assertion above still passing.
  const jsx = readFileSync(join(HERE, "..", "static", "dashboard.jsx"), "utf8");
  const chain = jsx.slice(jsx.indexOf("const ap2Text ="),
                          jsx.indexOf("const ap2Tone ="));
  assert.ok(chain.indexOf("receiverRunning === false") > 0,
    "the flavour line no longer consults the receiver at all");
  assert.ok(chain.indexOf("receiverRunning === false")
            < chain.indexOf("devAirplay2Ok"),
    "a down receiver must be answered before any clock verdict is printed");
}

for (const key of ["devAirplayFlavour", "devAirplayClassic",
                   "devAirplayUnknownFlavour", "devAirplay2Ok",
                   "devAirplay2ClockDown", "devAirplay2NoClock",
                   "devAirplay2ClockUnknown", "devAirplay2RxDown",
                   "devAirplay2RxDownNoClock"]) {
  const n = (strings.match(new RegExp(`^\\s*${key}:`, "gm")) || []).length;
  assert.strictEqual(n, 2,
    `${key} should be defined once per language in strings.js, found ${n}`);
}

console.log("airplay2_gate: all checks passed");
