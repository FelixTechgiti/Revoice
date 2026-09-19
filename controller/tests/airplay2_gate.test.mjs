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

console.log("airplay2_gate: all checks passed");
