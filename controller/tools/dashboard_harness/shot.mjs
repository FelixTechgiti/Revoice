// Screenshot the harness pages, and report anything the page threw.
//
// The errors are the half worth reading: React catches a render throw and
// leaves a blank page, so a component that breaks looks exactly like one
// that has not loaded yet. Every pageerror and console error is printed
// whether or not the screenshot looks fine.
import { createServer } from 'http';
import { readFile } from 'fs/promises';
import { existsSync } from 'fs';
import { dirname, join, extname, normalize } from 'path';
import { fileURLToPath } from 'url';
import puppeteer from 'puppeteer-core';

const HERE = dirname(fileURLToPath(import.meta.url));
const OUT = join(HERE, 'out');

// Where a Chromium usually is. CHROME overrides; the list is named in the
// error rather than "Chrome not found", so somebody with it installed
// elsewhere can see what was tried.
const CANDIDATES = [
  process.env.CHROME,
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files\\Google\\Chrome Beta\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
].filter(Boolean);
const CHROME = CANDIDATES.find(p => existsSync(p));
if (!CHROME) {
  console.error('No Chromium found. Set CHROME=/path/to/chrome.\nLooked in:\n  '
                + CANDIDATES.join('\n  '));
  process.exit(1);
}

// A file server rather than file://, because the page fetches its own
// scripts and a file:// origin refuses some of that.
const TYPES = { '.html': 'text/html', '.js': 'text/javascript',
                '.css': 'text/css', '.png': 'image/png', '.svg': 'image/svg+xml' };
const STATIC = join(HERE, '..', '..', 'static');
const server = createServer(async (req, res) => {
  const rel = normalize(decodeURIComponent(req.url.split('?')[0]))
                .replace(/^[/\\]+/, '');
  // Everything the pages reference lives in out/, except boot.js beside this
  // file and `static/…` — the favicon, which is served from the real
  // directory rather than stubbed, since the page asks for it by the same
  // path the controller does.
  const file = rel.endsWith('boot.js') ? join(HERE, 'boot.js')
             : rel.startsWith('static') ? join(STATIC, rel.slice(7))
             : join(OUT, rel);
  try {
    const body = await readFile(file);
    res.writeHead(200, { 'Content-Type': TYPES[extname(file)] || 'text/plain' });
    res.end(body);
  } catch {
    res.writeHead(404).end('not found');
  }
});
await new Promise(r => server.listen(0, '127.0.0.1', r));
const port = server.address().port;

const browser = await puppeteer.launch({
  executablePath: CHROME,
  headless: 'new',
  args: ['--force-color-profile=srgb', '--font-render-hinting=none'],
});

// Click the row whose name starts with `name`. The rows carry no id — they
// are what a person clicks, so this clicks what a person would.
async function openDevice(page, name) {
  await page.evaluate(n => {
    const row = [...document.querySelectorAll('div')].find(
      el => el.innerText?.startsWith(n) && el.onclick);
    if (!row) throw new Error(`no row for ${n}`);
    row.click();
  }, name);
  await new Promise(r => setTimeout(r, 600));
}

// Click a button by its visible text. Used for the panels that are not a
// device: the wizard and settings.
async function clickText(page, text) {
  await page.evaluate(s => {
    // The label is often not the whole of the element's text — the setup
    // row is a plus sign and a phrase — so match on containment, and keep
    // the candidate short so a container holding the whole page does not
    // win over the control itself.
    const el = [...document.querySelectorAll('button,div')].find(e => {
      const txt = e.innerText?.trim() || '';
      return txt.includes(s) && txt.length < s.length + 8
             && (e.tagName === 'BUTTON' || e.onclick);
    });
    if (!el) throw new Error(`nothing to click labelled ${s}`);
    el.click();
  }, text);
  await new Promise(r => setTimeout(r, 900));
}

// Every state a screenshot can show: both themes, both densities, the width
// where the sidebar goes under the list, and the two windows — a playing
// device and one waiting to be approved.
const SHOTS = [
  ['dark',    'dark.html',  1440, 1000, null,    null],
  ['light',   'light.html', 1440, 1000, null,    null],
  ['narrow',  'dark.html',   620, 1100, null,    null],
  ['dense',   'dark.html',  1440,  700, 'dense', null],
  ['device',  'dark.html',  1440,  900, null,    'Lounge'],
  ['approve', 'light.html', 1440,  900, null,    'G090LF1180570XYZ'],
  ['settings', 'dark.html', 1440,  900, null,    null, 'Settings'],
  ['wizard',   'dark.html', 1440,  900, null,    null, 'Set up an Echo Dot'],
];

let problems = 0;
for (const [name, page_, w, h, density, open, click] of SHOTS) {
  const page = await browser.newPage();
  // Set every time, not only for the dense shot: localStorage is per ORIGIN
  // and these pages share one, so a preference left by an earlier shot
  // silently decided the layout of the ones after it.
  await page.evaluateOnNewDocument(d => {
    try { localStorage.setItem('em-density', d); } catch (e) {}
  }, density || 'roomy');
  const errs = [];
  page.on('pageerror', e => errs.push(String(e)));
  page.on('console', m => { if (m.type() === 'error') errs.push(m.text()); });
  await page.setViewport({ width: w, height: h, deviceScaleFactor: 2 });
  await page.goto(`http://127.0.0.1:${port}/${page_}`, { waitUntil: 'networkidle0' });
  await new Promise(r => setTimeout(r, 1200));
  if (open) await openDevice(page, open);
  if (click) await clickText(page, click);
  // A modal is position:fixed, so fullPage would shoot the page BEHIND it
  // at full height with the window floating over the first screenful.
  await page.screenshot({ path: join(OUT, `shot-${name}.png`), fullPage: !(open || click) });

  const text = await page.evaluate(() => document.getElementById('root').innerText);
  console.log(`\n── ${name} → out/shot-${name}.png`);
  if (errs.length) {
    problems += errs.length;
    console.log('   ERRORS:\n     ' + errs.join('\n     '));
  }
  // An empty root is React having caught a throw. Say so rather than
  // leaving a blank PNG to be interpreted.
  if (!text.trim()) {
    problems++;
    console.log('   The page rendered NOTHING — see the errors above.');
  } else {
    console.log('   ' + text.split('\n').filter(Boolean).slice(0, 6).join(' | '));
  }
  await page.close();
}

await browser.close();
server.close();
console.log(problems ? `\n${problems} problem(s) — read them before trusting the PNGs.`
                     : '\nNo page errors.');
process.exit(problems ? 1 : 0);
