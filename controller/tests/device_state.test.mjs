// Tests for deviceState() and playbackSource() in dashboard.jsx — the one
// state a device row is in, and the colour that says so.
//
//     node controller/tests/device_state.test.mjs
//
// Source extraction rather than import, for the reason wifi_scan.test.mjs
// gives: the dashboard compiles to a single classic script with no module
// boundary, so the alternative is a second copy that drifts.
//
// What is under test is the ORDER, which is a contract rather than a
// preference. Voice outranks music because that is what the device's own
// mixer does — a turn DUCKS the music rather than pausing it, so while both
// are audible the voice is the thing being listened to. Put `playing` above
// the voice states and a Dot answering a question over a track reads as a
// speaker; put it below `idle` and it never renders at all.
//
// The second contract is that an ABSENT `audio` reads as "not playing" and
// never as unknown-therefore-playing. Old firmware cannot say what owns its
// music plane, and a row drawn as playing over a silent speaker is a wrong
// answer rather than a stale one.

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const HERE = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(HERE, "..", "static", "dashboard.jsx"), "utf8");

// Lifts a `function name(...) { ... }` declaration by brace matching. The
// arrow form in pm_verdict.test.mjs does not apply — these are declarations.
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

// The source-label map rides along: the labels are half of what `playing`
// reports, and a key renamed on one side only is silent.
function liftConst(name) {
  const start = src.indexOf(`const ${name} = {`);
  if (start < 0) throw new Error(`dashboard.jsx no longer defines ${name}`);
  const end = src.indexOf("\n};", start);
  if (end < 0) throw new Error(`could not find the end of ${name}`);
  return src.slice(start, end + 3);
}

const { deviceState, playbackSource, PLAYBACK_SOURCES } = await import(
  "data:text/javascript;base64," + Buffer.from(
    liftConst("PLAYBACK_SOURCES") + "\n"
    + liftFunction("playbackSource") + "\n"
    + liftFunction("deviceState") + "\n"
    + "export { deviceState, playbackSource, PLAYBACK_SOURCES };"
  ).toString("base64"));

let failures = 0;
function check(name, cond, detail) {
  if (cond) return;
  failures++;
  console.error(`FAIL: ${name}${detail ? `\n      ${detail}` : ""}`);
}

// A device that is connected, approved and doing nothing else, plus whatever
// the case under test adds.
const base = { approved: true, connected: true };
const playingSpotify = { ...base, audio: { active: true, source: "spotify" } };

// ── The order ────────────────────────────────────────────────────────────
check("a turn outranks the music underneath it",
  deviceState({ ...playingSpotify, speaking: true }).key === "speaking",
  "music is ducked under a response, not stopped — the voice is what is "
  + "being listened to");
check("thinking outranks playing",
  deviceState({ ...playingSpotify, thinking: true }).key === "thinking");
check("listening outranks playing",
  deviceState({ ...playingSpotify, listening: true }).key === "listening");
check("muted outranks playing",
  deviceState({ ...playingSpotify, muted: true }).key === "muted");
check("offline outranks playing",
  deviceState({ ...playingSpotify, connected: false }).key === "offline");
check("pending outranks everything",
  deviceState({ ...playingSpotify, approved: false }).key === "pending");
check("playing outranks idle",
  deviceState(playingSpotify).key === "playing",
  "otherwise the state never renders at all");

// ── Absence is not playing ───────────────────────────────────────────────
check("a device that has not reported audio is ready, not playing",
  deviceState(base).key === "idle");
check("audio present but inactive is ready",
  deviceState({ ...base, audio: { active: false, source: "none" } }).key === "idle");
check("an active VOICE source is not the playing state",
  deviceState({ ...base, audio: { active: true, source: "voice" } }).key === "idle",
  "voice is not a music source — the voice states above answer for it");
check("a source this dashboard does not know reads as silence",
  deviceState({ ...base, audio: { active: true, source: "gramophone" } }).key === "idle",
  "inventing a label for it puts a string nobody can read on screen");

// ── What the source says ─────────────────────────────────────────────────
check("the controller's own stream is named for who asked for it",
  playbackSource({ audio: { active: true, source: "media" } }) === "HA MEDIA");
for (const [key, label] of Object.entries(PLAYBACK_SOURCES)) {
  check(`${key} reports as ${label}`,
    playbackSource({ audio: { active: true, source: key } }) === label);
  check(`${key} carries its label onto the state`,
    deviceState({ ...base, audio: { active: true, source: key } }).source === label);
}
check("a state that is not playing carries no source",
  deviceState(base).source === undefined,
  "a source beside `Ready` is a claim about a speaker that is silent");

// ── The colours are tokens ───────────────────────────────────────────────
// The ring stopped depicting an LED, so there is one colour per state and it
// is themeable. A literal here is invisible in one of the two themes.
for (const d of [base, playingSpotify, { ...base, speaking: true },
                 { ...base, muted: true }, { ...base, connected: false },
                 { ...playingSpotify, approved: false }]) {
  const st = deviceState(d);
  check(`${st.key} is coloured by a token`,
    /^var\(--[a-z-]+\)$/.test(st.color), `got ${st.color}`);
}
check("voice and music do not share a colour",
  deviceState({ ...base, listening: true }).color
    !== deviceState(playingSpotify).color,
  "one Dot is a satellite and a speaker; the two roles must never be "
  + "confused at a glance");

if (failures) {
  console.error(`\n${failures} failure(s)`);
  process.exit(1);
}
console.log("device_state: all checks passed");
