// Tests for the fleet page's prose — the sentence at the top, the exception
// line under it, and the counts all three read from.
//
//     node controller/tests/fleet_summary.test.mjs
//
// Source extraction rather than import, for the reason wifi_scan.test.mjs
// gives: the dashboard compiles to a single classic script with no module
// boundary, so the alternative is a second copy that drifts.
//
// These are the largest text on the page and they are ASSEMBLED rather than
// written, which is how a page ends up saying "1 devices are ready" to
// somebody who came to it because something was wrong. And they are
// assembled DIFFERENTLY per language: the German sentence drops its second
// noun to "eines", the English one to a bare number, so the two are separate
// functions in static/strings.js and both are exercised here. A language
// that is only ever tested through its own translations is a language whose
// grammar nobody has checked.
//
// The counts are derived in one place for the same reason: the sentence, the
// readouts and the rows all read from `fleetSummary`, so they cannot
// disagree about how many devices are playing.

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const HERE = dirname(fileURLToPath(import.meta.url));
const STATIC = join(HERE, "..", "static");
const src = readFileSync(join(STATIC, "dashboard.jsx"), "utf8");

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

// strings.js is a classic script that assigns to the global, so it is
// imported as one rather than picked apart — which also means this test
// fails if it stops working outside a browser, and it has to work there:
// the landing page loads it before anything else exists.
const stringsSrc = readFileSync(join(STATIC, "strings.js"), "utf8");
const I18N = (await import("data:text/javascript;base64," + Buffer.from(
  stringsSrc + "\nexport default globalThis.EM_I18N;").toString("base64"))).default;

// fleetSummary calls deviceState, which calls playbackSource, which reads
// PLAYBACK_SOURCES — so the whole chain comes across rather than a stub of
// it. A stub is exactly where "how many are playing" would drift.
const mod = await import("data:text/javascript;base64," + Buffer.from([
  liftConst("PLAYBACK_SOURCES", "\n};"),
  liftFunction("playbackSource"),
  liftFunction("deviceState"),
  liftFunction("fleetSummary"),
  liftFunction("fleetSentence"),
  liftFunction("sinceText"),
  liftFunction("fleetException"),
  "export { fleetSummary, fleetSentence, sinceText, fleetException, deviceState };",
].join("\n")).toString("base64"));
const { fleetSummary, fleetSentence, sinceText, fleetException, deviceState } = mod;

// Every lifted function takes the strings object as its last argument, so
// nothing here depends on which language the machine running it is set to.
const EN = (() => { I18N.set("en"); return I18N.strings(); })();
const DE = (() => { I18N.set("de"); return I18N.strings(); })();

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
// ── Updates, which this page no longer decides ───────────────────────────
//
// The comparison moved to the server (em_updates) when emOS joined firmware
// as a track the fleet has to show (#255). It used to happen HERE, as a
// string inequality against the release — so the row, this summary and the
// device's own Updates tab were three rules that could disagree about one
// device. What is left to test is that the page reads the answer it was
// given and does not reconstruct one.
const upd = state => ({ state, tracks: {}, available: [], unknown: [] });
{
  const s = fleetSummary([
    dev({ updates: upd("available") }),
    dev({ updates: upd("current") }),
    dev({ updates: upd("unknown") }),
  ], { version: "v2.40.0" });
  eq("devices with something waiting", s.updates, 1);
  eq("devices nobody could check", s.updatesUnknown, 1);
}
{
  // The one number that must never absorb the other. A device that could not
  // be checked is not a device with nothing to do, and a summary that added
  // them would say "everything is current" about a fleet nobody has read.
  const s = fleetSummary([dev({ updates: upd("unknown") })], { version: "v2.40.0" });
  eq("an unreadable device does not count as an update", s.updates, 0);
  eq("and is not silently current either", s.updatesUnknown, 1);
}
{
  // A version inequality is no longer enough on its own — the server says
  // whether it is an update, and this page must not second-guess it. The
  // fixture is a device that LOOKS behind and is reported current (a build
  // ahead of the tag is the real case).
  const s = fleetSummary([
    dev({ firmware_ver: "v2.1.0", updates: upd("current") }),
  ], { version: "v2.40.0" });
  eq("the page does not re-derive the comparison", s.updates, 0);
}
eq("a server too old to send the field counts as neither",
  fleetSummary([dev({ firmware_ver: "v1.0.0" })], null).updates, 0);
eq("and not as unknown either — that is about a DEVICE, not a controller",
  fleetSummary([dev({ firmware_ver: "v1.0.0" })], null).updatesUnknown, 0);

// ── The sentence, in both languages ──────────────────────────────────────
const en = (devices, release) => fleetSentence(fleetSummary(devices, release), EN);
const de = (devices, release) => fleetSentence(fleetSummary(devices, release), DE);

eq("en: an empty fleet", en([]), "No devices yet.");
eq("de: an empty fleet", de([]), "Noch keine Geräte.");
eq("en: a pending device does not read as empty",
  en([dev({ approved: false })]), "Something new is waiting for you.");
eq("de: a pending device does not read as empty",
  de([dev({ approved: false })]), "Etwas Neues wartet auf dich.");
eq("en: one device, offline", en([dev({ connected: false })]), "Your device is offline.");
eq("de: one device, offline", de([dev({ connected: false })]), "Dein Gerät ist offline.");
eq("en: every device offline",
  en([dev({ connected: false }), dev({ connected: false })]), "Every device is offline.");
eq("de: every device offline",
  de([dev({ connected: false }), dev({ connected: false })]), "Alle Geräte sind offline.");
eq("en: one device, ready", en([dev()]), "One device is ready.");
eq("de: one device, ready", de([dev()]), "Ein Gerät ist bereit.");
eq("en: several ready", en([dev(), dev(), dev()]), "Three devices are ready.");
eq("de: several ready", de([dev(), dev(), dev()]), "Drei Geräte sind bereit.");

// The handoff's own example, which is where the two grammars part company.
eq("en: the handoff's example",
  en([dev({ listening: true }), playing(), dev(), dev()]),
  "One device is listening, one is playing music.");
eq("de: the handoff's example",
  de([dev({ listening: true }), playing(), dev(), dev()]),
  "Ein Gerät hört zu, eines spielt Musik.");

eq("en: only music", en([playing(), dev()]), "One device is playing music.");
eq("de: only music", de([playing(), dev()]), "Ein Gerät spielt Musik.");

// Plural agreement in both clauses, and in German the verb moves.
eq("en: plural agrees in both clauses",
  en([dev({ speaking: true }), dev({ thinking: true }), playing(), playing()]),
  "Two devices are listening, two are playing music.");
eq("de: plural agrees in both clauses",
  de([dev({ speaking: true }), dev({ thinking: true }), playing(), playing()]),
  "Zwei Geräte hören zu, zwei davon spielen Musik.");

// The readouts carry the digits; the sentence carries the words, up to the
// point where a word costs more to read than a number.
const twelve = Array.from({ length: 12 }, () => dev());
eq("en: past nine the digit wins", en(twelve), "12 devices are ready.");
eq("de: past nine the digit wins", de(twelve), "12 Geräte sind bereit.");

check("an offline device is never counted as ready",
  !en([dev(), dev({ connected: false })]).includes("Two"),
  "the sentence is about what is happening, and an offline device is not");

// ── The exception line ───────────────────────────────────────────────────
const NOW = 1_800_000_000_000;
const agoMin = m => Math.floor((NOW - m * 60_000) / 1000);
const exc = (ds, rel, L) => fleetException(ds, fleetSummary(ds, rel), NOW, L);

eq("en: a quiet fleet says so", exc([dev(), dev()], null, EN), "Nothing needs attention");
eq("de: a quiet fleet says so", exc([dev(), dev()], null, DE), "Nichts zu tun");

{
  const ds = [dev(), dev({ label: "Office", connected: false, last_seen: agoMin(14) })];
  eq("en: an offline device is named, with how long",
    exc(ds, null, EN), "Office offline for 14 minutes");
  eq("de: an offline device is named, with how long",
    exc(ds, null, DE), "Office offline seit 14 Minuten");
}
{
  // Past two, naming them stops being an exception and becomes the list.
  const off = n => dev({ label: `Dot ${n}`, connected: false, last_seen: agoMin(5) });
  const ds = [off(1), off(2), off(3), off(4)];
  eq("en: past two they are counted", exc(ds, null, EN),
    "Dot 1 offline for 5 minutes · Dot 2 offline for 5 minutes · 2 more offline");
  eq("de: past two they are counted", exc(ds, null, DE),
    "Dot 1 offline seit 5 Minuten · Dot 2 offline seit 5 Minuten · 2 weitere offline");
}
{
  // Track-neutral wording: the count includes emOS now, so "on older
  // firmware" would be false for a device whose firmware is current and
  // whose emOS is not — and it sends the reader to the wrong button.
  const ds = [dev({ updates: upd("available") }), dev({ approved: false })];
  eq("en: waiting and behind both make the line", exc(ds, { version: "v2.40.0" }, EN),
    "1 waiting for approval · 1 with updates");
  eq("de: waiting and behind both make the line", exc(ds, { version: "v2.40.0" }, DE),
    "1 wartet auf Freigabe · 1 mit Updates");
}
{
  // A device that has never connected has no last_seen. Saying "for 56 years"
  // is worse than admitting the gap.
  const ds = [dev({ label: "Never", connected: false, last_seen: null })];
  eq("en: no last_seen is stated as unknown, not computed from zero",
    exc(ds, null, EN), "Never offline for an unknown time");
  eq("de: no last_seen is stated as unknown, not computed from zero",
    exc(ds, null, DE), "Never offline seit unbekannter Zeit");
}
eq("an unnamed device falls back to its id",
  fleetException([dev({ connected: false, last_seen: agoMin(3) })],
                 { pending: 0, updates: 0 }, NOW, EN),
  "G090ABCD offline for 3 minutes");

// ── The duration ─────────────────────────────────────────────────────────
eq("en: a blink is not a duration", sinceText(agoMin(1), NOW, EN), "just now");
eq("de: a blink is not a duration", sinceText(agoMin(1), NOW, DE), "gerade eben");
eq("en: minutes", sinceText(agoMin(14), NOW, EN), "for 14 minutes");
eq("de: minutes", sinceText(agoMin(14), NOW, DE), "seit 14 Minuten");
eq("en: hours",   sinceText(agoMin(240), NOW, EN), "for 4 hours");
eq("de: hours",   sinceText(agoMin(240), NOW, DE), "seit 4 Stunden");
eq("en: days",    sinceText(agoMin(60 * 24 * 3), NOW, EN), "for 3 days");
eq("de: days",    sinceText(agoMin(60 * 24 * 3), NOW, DE), "seit 3 Tagen");
eq("a clock that ran backwards does not print a negative age",
  sinceText(Math.floor(NOW / 1000) + 500, NOW, EN), "just now");

// ── Every key exists in both languages ───────────────────────────────────
// A missing key falls back to English, which renders a real word and is
// therefore invisible — the failure this catches is a German build with
// English words scattered through it, which reads as sloppiness rather than
// as a bug and so gets reported by nobody.
{
  const enKeys = Object.keys(EN).sort();
  const deKeys = Object.keys(DE).sort();
  const missing = enKeys.filter(k => !(k in DE));
  const extra   = deKeys.filter(k => !(k in EN));
  check("every English key has a German one", missing.length === 0,
    `missing from de: ${missing.join(", ")}`);
  check("no German key is orphaned", extra.length === 0,
    `not in en: ${extra.join(", ")}`);
  // And they have to be the same KIND of thing: a function where the other
  // has a string is a call that returns undefined.
  const wrongType = enKeys.filter(k => k in DE && typeof EN[k] !== typeof DE[k]);
  check("the two languages agree on what each key is", wrongType.length === 0,
    `type mismatch: ${wrongType.join(", ")}`);
}

// ── The state labels resolve in both ─────────────────────────────────────
{
  const states = [
    [dev({ approved: false }), "statePending"],
    [dev({ connected: false }), "stateOffline"],
    [dev({ muted: true }), "stateMuted"],
    [dev({ speaking: true }), "stateSpeaking"],
    [dev({ thinking: true }), "stateThinking"],
    [dev({ listening: true }), "stateListening"],
    [playing(), "statePlaying"],
    [dev(), "stateIdle"],
  ];
  for (const [d, key] of states) {
    eq(`deviceState names the key for ${key}`, deviceState(d).labelKey, key);
    check(`${key} has a German word`, typeof DE[key] === "string" && DE[key].length > 0);
  }
}

if (failures) {
  console.error(`\n${failures} failure(s)`);
  process.exit(1);
}
console.log("fleet_summary: all checks passed");
