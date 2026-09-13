// Tests for the fleet page's prose in dashboard.jsx — the sentence at the top
// and the exception line under it.
//
//     node controller/tests/fleet_summary.test.mjs
//
// Source extraction rather than import, for the reason wifi_scan.test.mjs
// gives: the dashboard compiles to a single classic script with no module
// boundary, so the alternative is a second copy that drifts.
//
// These are the largest text on the page and they are ASSEMBLED rather than
// written, which is how a page ends up saying "1 devices are ready" to
// somebody who came to it because something was wrong. The counts are
// derived in one place for the same reason: the sentence, the readouts and
// the rows all read from `fleetSummary`, so they cannot disagree about how
// many devices are playing.

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const HERE = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(HERE, "..", "static", "dashboard.jsx"), "utf8");

function liftFunction(name) {
  const start = src.indexOf(`function ${name}(`);
  if (start < 0) {
    throw new Error(`dashboard.jsx no longer defines ${name}() — if it was `
                  + `renamed or moved, update this test to match`);
  }
  let depth = 0, seen = false;
  for (let i = start; i < src.length; i++) {
    const ch = src[i];
    if (ch === "{") { depth++; seen = true; }
    else if (ch === "}") {
      depth--;
      if (seen && depth === 0) return src.slice(start, i + 1);
    }
  }
  throw new Error(`could not find the end of ${name}`);
}

function liftConst(name, close) {
  const start = src.indexOf(`const ${name} = `);
  if (start < 0) throw new Error(`dashboard.jsx no longer defines ${name}`);
  const end = src.indexOf(close, start);
  if (end < 0) throw new Error(`could not find the end of ${name}`);
  return src.slice(start, end + close.length);
}

// fleetSummary calls deviceState, which calls playbackSource, which reads
// PLAYBACK_SOURCES — so the whole chain comes across rather than a stub of
// it. A stub is exactly where "how many are playing" would drift.
const mod = await import("data:text/javascript;base64," + Buffer.from([
  liftConst("PLAYBACK_SOURCES", "\n};"),
  liftFunction("playbackSource"),
  liftFunction("deviceState"),
  liftConst("_WORDS", "'nine'];"),
  liftFunction("_word"),
  liftFunction("_cap"),
  liftFunction("fleetSummary"),
  liftFunction("fleetSentence"),
  liftFunction("sinceText"),
  liftFunction("fleetException"),
  "export { fleetSummary, fleetSentence, sinceText, fleetException };",
].join("\n")).toString("base64"));
const { fleetSummary, fleetSentence, sinceText, fleetException } = mod;

let failures = 0;
function check(name, cond, detail) {
  if (cond) return;
  failures++;
  console.error(`FAIL: ${name}${detail ? `\n      ${detail}` : ""}`);
}
function eq(name, got, want) {
  check(name, got === want, `got  ${JSON.stringify(got)}\n      want ${JSON.stringify(want)}`);
}

const dev = (o = {}) => ({ approved: true, connected: true, device_id: "G090ABCDEF", ...o });
const playing = (o = {}) => dev({ audio: { active: true, source: "spotify" }, ...o });

// ── The counts ───────────────────────────────────────────────────────────
{
  const s = fleetSummary([
    dev({ label: "Kitchen", listening: true }),
    playing({ label: "Lounge" }),
    dev({ label: "Office", connected: false }),
    dev({ label: "New", approved: false }),
  ], { version: "v2.40.0" });
  eq("approved excludes the pending one", s.approved, 3);
  eq("online counts the connected approved ones", s.online, 2);
  eq("in conversation", s.active, 1);
  eq("playing", s.playing, 1);
  eq("waiting", s.pending, 1);
}
{
  // A device with no firmware reported yet is not "behind" — it has not said.
  const s = fleetSummary([
    dev({ firmware_ver: "v2.39.0" }),
    dev({ firmware_ver: "v2.40.0" }),
    dev({ firmware_ver: null }),
  ], { version: "v2.40.0" });
  eq("only a KNOWN older version counts as an update", s.updates, 1);
}
eq("no release means nothing is behind",
  fleetSummary([dev({ firmware_ver: "v1.0.0" })], null).updates, 0);

// ── The sentence ─────────────────────────────────────────────────────────
const sentence = (devices, release) => fleetSentence(fleetSummary(devices, release));

eq("an empty fleet", sentence([]), "No devices yet.");
eq("nothing but a pending device does not read as empty",
  sentence([dev({ approved: false })]), "Something new is waiting for you.");
eq("one device, offline", sentence([dev({ connected: false })]),
  "Your device is offline.");
eq("every device offline",
  sentence([dev({ connected: false }), dev({ connected: false })]),
  "Every device is offline.");
eq("one device, ready", sentence([dev()]), "One device is ready.");
eq("several ready", sentence([dev(), dev(), dev()]), "Three devices are ready.");
eq("the handoff's own example",
  sentence([dev({ listening: true }), playing(), dev(), dev()]),
  "One device is listening, one is playing music.");
eq("only music", sentence([playing(), dev()]), "One device is playing music.");
eq("plural agrees in both clauses",
  sentence([dev({ speaking: true }), dev({ thinking: true }), playing(), playing()]),
  "Two devices are listening, two are playing music.");
// The readouts carry the digits; the sentence carries the words, up to the
// point where a word costs more to read than a number.
eq("past nine the digit wins",
  sentence(Array.from({ length: 12 }, () => dev())), "12 devices are ready.");

check("an offline device is never counted as ready",
  !sentence([dev(), dev({ connected: false })]).includes("Two"),
  "the sentence is about what is happening, and an offline device is not");

// ── The exception line ───────────────────────────────────────────────────
const NOW = 1_800_000_000_000;
const agoMin = m => Math.floor((NOW - m * 60_000) / 1000);

eq("a quiet fleet says so",
  fleetException([dev(), dev()], fleetSummary([dev(), dev()], null), NOW),
  "Nothing needs attention");

{
  const ds = [dev(), dev({ label: "Office", connected: false, last_seen: agoMin(14) })];
  eq("an offline device is named, with how long",
    fleetException(ds, fleetSummary(ds, null), NOW),
    "Office offline for 14 minutes");
}
{
  // Past two, naming them stops being an exception and becomes the list.
  const off = n => dev({ label: `Dot ${n}`, connected: false, last_seen: agoMin(5) });
  const ds = [off(1), off(2), off(3), off(4)];
  eq("past two they are counted",
    fleetException(ds, fleetSummary(ds, null), NOW),
    "Dot 1 offline for 5 minutes · Dot 2 offline for 5 minutes · 2 more offline");
}
{
  const ds = [dev({ firmware_ver: "v2.1.0" }), dev({ approved: false })];
  eq("waiting and behind both make the line",
    fleetException(ds, fleetSummary(ds, { version: "v2.40.0" }), NOW),
    "1 waiting for approval · 1 on older firmware");
}
{
  // A device that has never connected has no last_seen. Saying "for 56 years"
  // is worse than admitting the gap.
  const ds = [dev({ label: "Never", connected: false, last_seen: null })];
  eq("no last_seen is stated as unknown, not computed from zero",
    fleetException(ds, fleetSummary(ds, null), NOW),
    "Never offline for an unknown time");
}
eq("an unnamed device falls back to its id",
  fleetException([dev({ connected: false, last_seen: agoMin(3) })],
                 { pending: 0, updates: 0 }, NOW),
  "G090ABCD offline for 3 minutes");

// ── The duration ─────────────────────────────────────────────────────────
eq("a blink is not a duration", sinceText(agoMin(1), NOW), "just now");
eq("minutes",  sinceText(agoMin(14), NOW), "for 14 minutes");
eq("hours",    sinceText(agoMin(240), NOW), "for 4 hours");
eq("days",     sinceText(agoMin(60 * 24 * 3), NOW), "for 3 days");
eq("a clock that ran backwards does not print a negative age",
  sinceText(Math.floor(NOW / 1000) + 500, NOW), "just now");

if (failures) {
  console.error(`\n${failures} failure(s)`);
  process.exit(1);
}
console.log("fleet_summary: all checks passed");
