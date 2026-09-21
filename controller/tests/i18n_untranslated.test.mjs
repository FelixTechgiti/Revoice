// Which parts of the dashboard still speak English at the user.
//
// Every string the dashboard shows has to exist in both languages
// (`strings.js`, reached through `t(key)`), and the way this goes wrong is
// never a whole pane — it is four words left behind in a pane that looks
// finished. "+ Custom model" and "Sensitivity" sat in the middle of a fully
// German wake word section for a release, and the pane was signed off from a
// screenshot that did not scroll that far.
//
// So this is a RATCHET rather than a pass/fail: `i18n_budget.json` records
// how many English strings each component still has, a component may only
// ever go down, and one at 0 can never go back up. That is what makes
// finishing the translation a series of small changes instead of one sweep
// nobody can review.
//
// It reads two positions, because those are the two that render text:
//   - a JSX text node, `>like this</div>`
//   - a literal passed to a prop that is displayed (`label`, `sub`, …)
// Anything inside braces is an expression and is left alone: `{t('key')}` is
// the answer, and `{cond ? 'A' : 'B'}` is caught by the literal rule below.
import { readFileSync, writeFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const HERE = dirname(fileURLToPath(import.meta.url));
const JSX = join(HERE, '..', 'static', 'dashboard.jsx');
const BUDGET = join(HERE, 'i18n_budget.json');

// Strings that are not prose: units, protocol names, product names, and the
// handoff's own rule that units stay untranslated. Matched whole, after
// trimming, so "Hz" is exempt and "Hz of headroom" is not.
const NOT_PROSE = new Set([
  'dBm', 'dB', 'ms', 'Hz', 'kHz', 'MHz', 'GHz', '%', '×', '·', '—', '–', '↑', '▶',
  'Revoice', 'Home Assistant', 'Spotify', 'AirPlay', 'sendspin', 'Sendspin',
  'openwakeword', 'oww_forge', 'ESPHome', 'emOS', 'FireOS', 'TWRP', 'WiFi',
  'Wi-Fi', 'ADB', 'adb', 'USB', 'TLS', 'wss', 'md5', 'CPU', 'RAM', 'IP',
  // The wordmark is the logo: `Rev` and `ice` are the two halves the ring
  // sits between, not words anybody reads.
  'Rev', 'ice',
  // A command somebody types. Translating it would make it wrong.
  'docker compose pull &amp;&amp; docker compose up -d',
  // A unit, which the handoff keeps untranslated — and German abbreviates
  // minutes the same way, so there is nothing to carry.
  'min',
  // Names of files on the operator's own disk, written by the escrow step.
  // A translated filename is a file nobody has.
  'revoice-stock-boot-*.img', 'revoice-boot-before-patch-*.img',
]);

// A string is prose when it carries a word of three letters or more. That
// keeps out style values, css identifiers and format strings, which is the
// distinction a "does it look English" test otherwise gets wrong — and it is
// deliberately generous, because a false positive costs one allowlist entry
// and a false negative is the bug this file exists for.
// Module-scope values that are payloads rather than prose. `_INIT_RC_APPEND`
// is the init.rc fragment the wizard writes onto the device: translating a
// line of it would produce a service Android cannot start.
const EXEMPT_OWNERS = new Set(['_INIT_RC_APPEND']);

const PROSE = /[A-Za-z]{3,}/;
// A JSX text node spanning several lines has no angle brackets on the
// lines in the middle, so the between-the-brackets rule cannot see them —
// and a paragraph is exactly where the long, consequence-bearing
// sentences live. A line made only of words and punctuation is a text
// node by elimination: the same line in JavaScript would not parse.
//
// The class deliberately excludes the colon and both straight quotes,
// which is what separates a sentence from a line of a style object:
// `alignItems: 'center', gap: 7,` is letters and punctuation too, and
// without that exclusion this rule reported 388 strings, most of them
// CSS. The cost is a sentence written with a straight apostrophe, which
// this tree does not write.
const BARE_PROSE = /^\s*[A-Za-z(“][A-Za-z0-9 ,.;’“”()\-—–…!?%&]*$/;
// Comparison and boolean operators mean the `>` was an operator, not a
// tag: `psk.length >= 8 && psk.length` is not something anybody reads.
const IS_CODE = /&&|\|\||=>|===|!==|^\s*[=<>]/;
// A statement reads as prose to the rule above — `onStarted(res);` is
// letters and punctuation — so the two shapes a statement has and a
// sentence does not are excluded: a keyword at the front, a semicolon at
// the end.
const IS_STATEMENT = /;\s*$|^\s*(if|return|const|let|var|for|while|else|import|export|await|throw)\b/;
// A call and a property access are the two remaining code shapes that
// survive the rules above: `setScan(await API.get(` and
// `device.connected` are both letters and punctuation. A bracket
// straight after a letter is a call; prose puts a space before one.
// The fourth position text comes from, and the one a `>...<` rule cannot
// see at all: a button whose label depends on state, written as
// `{busy ? 'Saving…' : 'Save'}`. Both arms are UI text. Matched as the
// shape rather than as any string literal, because a literal on its own
// is far more often a css value than a sentence.
const TERNARY_LABELS = /\?\s*(['"])(.+?)\1\s*:\s*(['"])(.+?)\3/g;

const IS_CALL = /[A-Za-z_$]\(/;
const IS_MEMBER = /^[a-z_$][\w$]*\.[\w$]+$/;
// A css value is two words too: `2px solid transparent` is a border, not
// a sentence, and it reaches the ternary rule as both of its arms.
const IS_CSS = /\d(px|em|rem|%)\b|\bvar\(--|\b(solid|dashed|transparent|inherit|none|auto|inset)\b/;
const DISPLAY_PROP = /\b(label|sub|title|placeholder|note|unit|desc)\s*=\s*(['"])(.*?)\2/g;

function stripComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, m => m.replace(/[^\n]/g, ' '))
            .replace(/(?<!:)\/\/[^\n]*/g, '');
}

function scan() {
  const src = stripComments(readFileSync(JSX, 'utf8'));
  const found = {};
  let owner = '(module)';
  // Split on either ending. A Windows working tree is CRLF (.gitattributes
  // normalises on the way in), so an end-of-line anchor matched in CI and
  // silently did not here: the paragraph rule below found nothing at all
  // on the machine it was written on.
  for (const line of src.split(/\r?\n/)) {
    const decl = /^(?:function|const|let|class) ([A-Za-z_$][\w$]*)/.exec(line);
    if (decl) owner = decl[1];
    if (EXEMPT_OWNERS.has(owner)) continue;
    const hits = [];
    for (const m of line.matchAll(/>([^<>{}\n]*)</g)) hits.push(m[1]);
    for (const m of line.matchAll(DISPLAY_PROP)) hits.push(m[3]);
    for (const m of line.matchAll(TERNARY_LABELS)) {
      // Only an arm that reads as a PHRASE. A ternary between two
      // technical tokens is the far commoner shape here —
      // `freq >= 4900 ? '5GHz' : '2.4GHz'`, `tls ? 'wss' : 'ws'`, a colour, a
      // css keyword — and counting those buries the handful of button
      // labels this rule exists for. A space or a trailing ellipsis is
      // what a label has and a token does not.
      for (const arm of [m[2], m[4]]) {
        // A CHAINED ternary defeats the lazy match: the arm comes back as
        // `ok' : s.wifiRssi > -70 ? 'ok' : 'warn`, which is three arms and
        // two operators. A real label carries no quote and no operator.
        if (/['"]|\s[?:]\s/.test(arm)) continue;
        if (/ |…$/.test(arm)) hits.push(arm);
      }
    }
    // Two words at least, so a lone identifier on its own line is not
    // mistaken for a sentence.
    if (BARE_PROSE.test(line) && !IS_STATEMENT.test(line)
        && /[A-Za-z]{3,}[^A-Za-z]+[A-Za-z]{3,}/.test(line)) {
      hits.push(line);
    }
    for (const raw of hits) {
      const s = raw.trim().replace(/^[+·—–]\s*/, '');
      if (!s || !PROSE.test(s) || NOT_PROSE.has(s) || IS_CODE.test(s)
          || IS_CALL.test(s) || IS_MEMBER.test(s)
          || IS_CSS.test(s)) continue;
      (found[owner] ||= []).push(s);
    }
  }
  return found;
}

const found = scan();
const counts = Object.fromEntries(
  Object.entries(found).map(([k, v]) => [k, v.length]).sort());

if (process.argv.includes('--update')) {
  writeFileSync(BUDGET, JSON.stringify(counts, null, 2) + '\n');
  console.log(`i18n budget re-recorded: ${Object.keys(counts).length} components, `
              + `${Object.values(counts).reduce((a, b) => a + b, 0)} strings`);
  process.exit(0);
}

const budget = JSON.parse(readFileSync(BUDGET, 'utf8'));
const over = Object.entries(counts)
  .filter(([name, n]) => n > (budget[name] ?? 0))
  .map(([name, n]) => `  ${name}: ${budget[name] ?? 0} -> ${n}\n`
                      + found[name].slice(0, 6).map(s => `      ${s}`).join('\n'));

if (over.length) {
  console.error('English strings increased. Put them in strings.js and reach them\n'
                + 'through t(key); both languages must carry the key.\n' + over.join('\n'));
  process.exit(1);
}
const total = Object.values(counts).reduce((a, b) => a + b, 0);
const budgeted = Object.values(budget).reduce((a, b) => a + b, 0);
console.log(`i18n ratchet: ${total} English strings left (budget ${budgeted}).`);
if (total < budgeted) {
  console.log('Below budget — re-record it with `node tests/i18n_untranslated.test.mjs --update`.');
}
