// Every module-private the dashboard CALLS must also be DEFINED in it.
//
//     node controller/tests/dashboard_definitions.test.mjs
//
// This exists because the build cannot notice. The dashboard is compiled with
// `esbuild --bundle=false --jsx=transform`, which rewrites JSX and nothing
// else: it never resolves an identifier, so a helper whose definition is gone
// compiles exactly as cleanly as one that is still there. The failure then
// waits in the browser, on the one click that reaches the dead call.
//
// That is not hypothetical. The redesign commit 2806f5c deleted the 145-line
// `_ADB` block — the whole ADB-over-WebUSB client — and left its three call
// sites and its comment header standing. CI stayed green, the page loaded, the
// dashboard worked, and the ONE thing that broke was the provisioning wizard's
// Connect step: `_ADB.Client.requestDevice()` threw a TypeError before the USB
// picker could open, so the browser showed no dialog at all. Reported from
// hardware as "Chrome asks nothing", with a working cable and a working device
// — which is indistinguishable from a cable fault until somebody reads the
// console.
//
// The rule is deliberately about the SHAPE rather than about `_ADB`: any
// `_Name` used at module scope is a module-private this file owns, so the
// condition is "referenced here, defined here", and pinning the one name that
// was lost would not have caught the next one.

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const HERE = dirname(fileURLToPath(import.meta.url));
const JSX = join(HERE, "..", "static", "dashboard.jsx");
const src = readFileSync(JSX, "utf8");

// Comments are stripped; strings deliberately are NOT. Pairing quotes in JSX
// is not something a regex can do — one apostrophe in prose ("it's") swallows
// everything up to the next quote — and getting that wrong makes the test
// report whatever the damage happened to leave behind. The reference shape
// below is narrow enough that a string cannot fake it.
const code = src
  .replace(/\/\*[\s\S]*?\*\//g, " ")
  .replace(/^[ \t]*\/\/.*$/gm, " ")
  .replace(/([^:])\/\/.*$/gm, "$1");

// A REFERENCE is `_Name.` or `_Name(` — the two shapes that throw a TypeError
// the moment the definition is gone, which is exactly the failure this test is
// for. A bare mention cannot: `target="_blank"` is a string, not a call.
// Property accesses are excluded by requiring no `.` in front, so somebody
// else's `x._field` is not our problem.
const USE = /(^|[^.\w$])(_[A-Za-z][\w$]*)\s*[.(]/g;
// `const _X`, `let _X`, `var _X`, `function _X`, `class _X`, and the
// destructuring and parameter forms a local can take.
const DEF = /(?:\b(?:const|let|var|function|class)\s+|[({[,]\s*)(_[A-Za-z][\w$]*)/g;
// A class method — `_pump() {`, `async _send(line) {` — which carries no
// declaration keyword and is therefore invisible to DEF above. Its own
// definition line matches the reference shape, so without this a method that
// is only ever called on `this` reports as missing from the file that defines
// it.
const METHOD = /^\s*(?:static\s+)?(?:async\s+)?(?:get\s+|set\s+)?(_[A-Za-z][\w$]*)\s*\(/gm;

function names(re) {
  const out = new Set();
  let m;
  while ((m = re.exec(code)) !== null) out.add(m[2] ?? m[1]);
  return out;
}

const used = names(USE);
const defined = new Set([...names(DEF), ...names(METHOD)]);

// Globals the page genuinely gets from elsewhere would go here. The list is
// empty on purpose: everything underscore-prefixed in this file is its own.
const EXTERNAL = new Set([]);

const missing = [...used].filter((n) => !defined.has(n) && !EXTERNAL.has(n)).sort();

let failed = 0;

if (missing.length) {
  failed = 1;
  console.error(
    `FAIL dashboard.jsx references ${missing.length} module-private(s) it does `
    + `not define:\n`
    + missing.map((n) => `  ${n}`).join("\n")
    + `\n\nEither the definition was deleted with its call sites left behind `
    + `(see 2806f5c), or the name really does come from outside — in which `
    + `case add it to EXTERNAL in this test and say where it comes from.`,
  );
} else {
  console.log(`ok  every module-private is defined (${used.size} referenced)`);
}

// The check above is a net; this is the specific thing it was cast for. A
// regex that stopped matching would report success on an empty set, so one
// name that MUST be found keeps the net honest.
if (!/\bconst _ADB\s*=/.test(code)) {
  failed = 1;
  console.error(
    "FAIL dashboard.jsx defines no _ADB — the wizard's ADB-over-WebUSB client "
    + "is gone, so Connect Device cannot open the USB picker at all.",
  );
} else if (!used.has("_ADB")) {
  failed = 1;
  console.error(
    "FAIL nothing references _ADB any more. If the wizard moved to another ADB "
    + "client this test should point at that one instead; if the reference "
    + "extraction broke, the check above is passing on an empty set.",
  );
} else {
  console.log("ok  _ADB is both defined and used");
}

process.exit(failed);
