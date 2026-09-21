// The mark a device row shows about updates, in both languages.
//
//     node controller/tests/update_badge.test.mjs
//
// Source extraction rather than import, for the reason fleet_summary does it:
// the dashboard compiles to one classic script with no module boundary, so
// the alternative is a second copy that drifts.
//
// THE POINT OF THIS FILE IS THE THIRD MARK. A device whose emOS version
// nobody could read must not render like a device with nothing waiting —
// blank is what "current" looks like, and absence read as currency is the
// whole reason the aggregated indicator was worth building (#255). Two of
// the three cases below exist only to stop a later simplification collapsing
// unknown into current, which no screenshot would catch.

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

const stringsSrc = readFileSync(join(STATIC, "strings.js"), "utf8");
const I18N = (await import("data:text/javascript;base64," + Buffer.from(
  stringsSrc + "\nexport default globalThis.EM_I18N;").toString("base64"))).default;

const mod = await import("data:text/javascript;base64," + Buffer.from([
  liftFunction("updateBadge"),
  "export { updateBadge };",
].join("\n")).toString("base64"));
const { updateBadge } = mod;

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

const withUpdates = (state, o = {}) => ({
  updates: { state, tracks: {}, available: [], unknown: [], ...o },
});

// ── The three marks ──────────────────────────────────────────────────────
eq("available carries an arrow",
  updateBadge(withUpdates("available", { available: ["firmware"] }), EN).mark, " ↑");
eq("unknown carries a question mark, never a blank",
  updateBadge(withUpdates("unknown", { unknown: ["emos"] }), EN).mark, " ?");
eq("current carries nothing",
  updateBadge(withUpdates("current"), EN).mark, "");

check("available is the warning colour",
  updateBadge(withUpdates("available"), EN).color === "var(--warn)");
check("unknown is muted rather than alarming — nobody looked, which is not a fault",
  updateBadge(withUpdates("unknown"), EN).color === "var(--muted)");
check("current has no colour of its own",
  updateBadge(withUpdates("current"), EN).color === null);

// ── The title names the tracks ───────────────────────────────────────────
//
// An OTA and a partition write are not interchangeable, so an arrow that
// does not say which is an arrow nobody can act on.
eq("en: one track named",
  updateBadge(withUpdates("available", { available: ["emos"] }), EN).title,
  "Update available: emOS");
eq("en: both tracks named, in declaration order",
  updateBadge(withUpdates("available", { available: ["firmware", "emos"] }), EN).title,
  "Update available: firmware, emOS");
eq("de: one track named",
  updateBadge(withUpdates("available", { available: ["emos"] }), DE).title,
  "Update verfügbar: emOS");
eq("de: both tracks named",
  updateBadge(withUpdates("available", { available: ["firmware", "emos"] }), DE).title,
  "Update verfügbar: Firmware, emOS");

// The unknown title has to SAY that unknown is not up to date, because the
// mark alone is a shrug and a shrug reads as fine.
check("en: the unknown title refuses the reassuring reading",
  updateBadge(withUpdates("unknown", { unknown: ["emos"] }), EN)
    .title.includes("not up to date"));
check("de: the unknown title refuses the reassuring reading",
  updateBadge(withUpdates("unknown", { unknown: ["emos"] }), DE)
    .title.includes("nicht aktuell"));

// ── A track name the page has never heard of ─────────────────────────────
//
// The server owns the track list, so a controller newer than this page can
// name one that is not in the table. Showing the raw key is ugly and
// truthful; dropping it would under-report an update.
eq("an unknown track key falls through rather than vanishing",
  updateBadge(withUpdates("available", { available: ["endpoints"] }), EN).title,
  "Update available: endpoints");

// ── A device the server said nothing about ───────────────────────────────
//
// null, so the caller falls back to its own label rather than rendering a
// badge built from nothing. An older controller sends no `updates` at all,
// and inventing "current" for it is the same lie one layer up.
eq("no updates field at all yields no badge",
  updateBadge({ firmware_ver: "v2.52.0-fx.1" }, EN), null);

if (failures) {
  console.error(`\n${failures} failure(s)`);
  process.exit(1);
}
console.log("update_badge: all checks passed");
