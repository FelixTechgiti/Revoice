// The login page's status line renders text, never markup.
//
//     node controller/tests/login_status.test.mjs
//
// The status line shows server error strings. Upstream's version of it built
// its span with innerHTML, so an error containing markup would have run as
// HTML on the page that takes the admin password. This fork's status line is
// one element and one setter rather than a ring plus a readout, so the test
// drives THAT function — lifted from the page, against a minimal DOM — but
// the property it holds is upstream's and the reason is the same.

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const HERE = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(HERE, "..", "static", "index.html"), "utf8");

function liftFunction(name) {
  const start = src.indexOf(`function ${name}(`);
  if (start < 0) throw new Error(`index.html no longer defines ${name}()`);
  let depth = 0;
  for (let i = src.indexOf("{", start); i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}" && --depth === 0) return src.slice(start, i + 1);
  }
  throw new Error(`could not find the end of ${name}`);
}

let innerHTMLWrites = 0;
const stripText = {
  textContent: "",
  set innerHTML(_) { innerHTMLWrites++; },
};
const strip = {
  className: "",
  set innerHTML(_) { innerHTMLWrites++; },
};
const OK = "ok";
const setStatus = new Function("stripText", "strip", "OK",
  `${liftFunction("setStatus")}\nreturn setStatus;`)(stripText, strip, OK);

let failures = 0;
function check(name, cond, detail) {
  if (cond) return;
  failures++;
  console.error(`FAIL: ${name}${detail ? `\n      ${detail}` : ""}`);
}

const hostile = '<img src=x onerror="alert(1)">';
setStatus(hostile, "error");

check("markup arrives as text", stripText.textContent === hostile,
      JSON.stringify(stripText.textContent));
check("nothing is written through innerHTML", innerHTMLWrites === 0);
check("the tone becomes a class, not markup", strip.className === "status status--error",
      strip.className);

setStatus("Signed in", OK);
check("a second call replaces the first", stripText.textContent === "Signed in");
check("the ok tone carries no modifier", strip.className === "status", strip.className);

if (failures) {
  console.error(`\n${failures} failure(s)`);
  process.exit(1);
}
console.log("login_status: all checks passed");
