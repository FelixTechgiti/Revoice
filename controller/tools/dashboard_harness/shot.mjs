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
// Which landing state the next page load will see. Set per shot, because
// first-run and ready are the two states of that page and they are the
// whole of what it can look like.
let LANDING_SETUP = false;
const server = createServer(async (req, res) => {
  const rel = normalize(decodeURIComponent(req.url.split('?')[0]))
                // normalize() turns / into \ on Windows, so the separator is
                // put back: every path compared below is written with forward
                // slashes, and without this the API routes matched on Linux
                // and silently did not here.
                .replace(/\\/g, '/').replace(/^\/+/, '');
  // The landing page is served as ITSELF rather than rebuilt, because it is
  // self-contained — its own tokens, its own script, no bundle — so there is
  // nothing to assemble. Its two API calls are answered below.
  if (rel.startsWith('api/')) {
    const setup = rel === 'api/system/setup-state';
    res.writeHead(setup ? 200 : 401, { 'Content-Type': 'application/json' });
    res.end(setup ? JSON.stringify({ needs_setup: LANDING_SETUP }) : '{}');
    return;
  }
  // Everything else the pages reference lives in out/, except boot.js beside
  // this file and `static/…` — the favicon and the fonts, served from the
  // real directory rather than stubbed, since the page asks for them by the
  // same paths the controller does.
  const file = rel === 'landing' ? join(STATIC, 'index.html')
             : rel.endsWith('boot.js') ? join(HERE, 'boot.js')
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

// Every state a screenshot can show: both themes, both densities, both
// languages, the width where the sidebar goes under the list, the two
// windows — a playing device and one waiting to be approved — and the
// landing page in its three states.
//
// Objects rather than positional tuples: there are seven things to say
// about a shot now, and `[…, null, null, null, 'dark', true]` is a row
// nobody can read.
//
// `lang` is set on EVERY shot rather than left to the browser. Chrome takes
// its locale from the machine, so the language of these screenshots was
// whatever the person running them happened to have — which is fine until
// somebody compares two runs.
const SHOTS = [
  { name: 'dark',     page: 'dark.html',  w: 1440, h: 1000 },
  { name: 'light',    page: 'light.html', w: 1440, h: 1000 },
  { name: 'german',   page: 'dark.html',  w: 1440, h: 1000, lang: 'de' },
  { name: 'narrow',   page: 'dark.html',  w: 620,  h: 1100 },
  { name: 'dense',    page: 'dark.html',  w: 1440, h: 700,  density: 'dense' },
  { name: 'dense-de', page: 'dark.html',  w: 1440, h: 700,  density: 'dense', lang: 'de' },
  { name: 'device',   page: 'dark.html',  w: 1440, h: 900,  open: 'Lounge' },
  { name: 'approve',  page: 'light.html', w: 1440, h: 900,  open: 'G090LF1180570XYZ' },
  { name: 'settings', page: 'dark.html',  w: 1440, h: 900,  click: 'Settings' },
  { name: 'wizard',   page: 'dark.html',  w: 1440, h: 900,  click: 'Set up an Echo Dot' },
  // The landing page, which is the first thing anyone sees and the only
  // page here that is not the dashboard.
  { name: 'landing-dark',  page: 'landing', w: 900, h: 640, theme: 'dark' },
  { name: 'landing-light', page: 'landing', w: 900, h: 640, theme: 'light' },
  { name: 'landing-de',    page: 'landing', w: 900, h: 640, theme: 'dark', lang: 'de' },
  { name: 'landing-setup', page: 'landing', w: 900, h: 760, theme: 'dark', setup: true },
];

let problems = 0;
for (const shot of SHOTS) {
  const { name, page: page_, w, h } = shot;
  const { density = 'roomy', lang = 'en', theme, open, click, setup } = shot;
  const page = await browser.newPage();
  // Set every time, not only for the dense shot: localStorage is per ORIGIN
  // and these pages share one, so a preference left by an earlier shot
  // silently decided the layout of the ones after it.
  await page.evaluateOnNewDocument((d, th, lg) => {
    try {
      localStorage.setItem('em-density', d);
      localStorage.setItem('em-lang', lg);
      // The landing page reads em-theme before first paint, the same way
      // dashboard.html does; the two dashboard pages carry the attribute in
      // their markup instead, so this is harmless there.
      if (th) localStorage.setItem('em-theme', th);
      // A stored session would send the landing page straight to the
      // dashboard, which is exactly what it is supposed to do and exactly
      // what cannot be photographed.
      localStorage.removeItem('em_token');
    } catch (e) {}
  }, density, theme || null, lang);
  LANDING_SETUP = !!setup;
  const errs = [];
  page.on('pageerror', e => errs.push(String(e)));
  page.on('console', m => {
    const txt = m.text();
    // "Failed to load resource: … 404" names nothing; the response hook
    // below reports the same misses with the path that missed.
    if (m.type() === 'error' && !txt.startsWith('Failed to load resource')) {
      errs.push(txt);
    }
  });
  // The vendored woff2 files are fetched into the image at build time, so a
  // checkout does not have them and the landing page's own @font-face rules
  // miss here by construction. Named rather than silenced: a blanket filter
  // on 404s would hide a missing script, which is what this is for.
  page.on('response', r => {
    if (r.status() === 404 && !r.url().includes('vendor/fonts/')) {
      errs.push('404 ' + new URL(r.url()).pathname);
    }
  });
  await page.setViewport({ width: w, height: h, deviceScaleFactor: 2 });
  await page.goto(`http://127.0.0.1:${port}/${page_}`, { waitUntil: 'networkidle0' });
  await new Promise(r => setTimeout(r, 1200));
  if (open) await openDevice(page, open);
  if (click) await clickText(page, click);
  // A modal is position:fixed, so fullPage would shoot the page BEHIND it
  // at full height with the window floating over the first screenful.
  await page.screenshot({ path: join(OUT, `shot-${name}.png`), fullPage: !(open || click) });

  // The landing page has no #root — it is not a React app.
  const text = await page.evaluate(
    () => (document.getElementById('root') || document.body).innerText);
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
