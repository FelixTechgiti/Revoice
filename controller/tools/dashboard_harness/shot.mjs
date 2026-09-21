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

// Chromium refuses to start as root without --no-sandbox, which is every
// container. Opt-in rather than always-on: the sandbox is worth keeping where
// there is one, and a person running this on their own machine should not
// silently lose it.
const NO_SANDBOX = process.env.CHROME_NO_SANDBOX === '1';

const browser = await puppeteer.launch({
  executablePath: CHROME,
  headless: 'new',
  args: ['--force-color-profile=srgb', '--font-render-hinting=none',
         ...(NO_SANDBOX ? ['--no-sandbox', '--disable-setuid-sandbox'] : [])],
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

// Scroll the modal's own scrolling region. The window is capped at
// min(700px, 90vh), so a taller viewport makes the PAGE taller and the
// window exactly as short as before — everything past the first section is
// unreachable by resizing, which is how a whole pane of tiles went
// unphotographed.
async function scrollModal(page, px) {
  await page.evaluate(n => {
    const box = [...document.querySelectorAll('.em-modal *')]
      .find(el => el.scrollHeight > el.clientHeight + 20);
    if (!box) throw new Error('nothing scrolls inside the window');
    box.scrollTop = n;
  }, px);
  await new Promise(r => setTimeout(r, 400));
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
  // The device window in German. It is the pane with the most strings
  // in the dashboard, so it is the one where four words left behind are
  // hardest to notice by reading the diff.
  { name: 'device-de', page: 'dark.html', w: 1440, h: 1100, open: 'Lounge', lang: 'de' },
  // The Updates tab of a device window. Added after a `{ ...label }` spread
  // of a name that pane's component does not have took the WHOLE dashboard
  // down the moment the tab was opened (2026-09-20) — a blank page that no
  // other shot could see, because none of them opened this tab.
  { name: 'updates',  page: 'dark.html',  w: 1440, h: 1200, open: 'Lounge',
    click: 'Updates' },
  { name: 'updates-de', page: 'dark.html', w: 1440, h: 1200, open: 'Lounge',
    click: 'Updates', lang: 'de' },
  { name: 'approve',  page: 'light.html', w: 1440, h: 900,  open: 'G090LF1180570XYZ' },
  // The muted device's own window, which is the only place the mark is
  // drawn at 44px. The fleet shots cover it at 34.
  { name: 'muted',    page: 'light.html', w: 1440, h: 900,  open: 'Bedroom' },
  { name: 'settings', page: 'dark.html',  w: 1440, h: 900,  click: 'Settings' },
  // The same pane in the other theme. Both bugs this shot was added for
  // were theme-specific and invisible in the other one: a text colour
  // that was the console's hairline, and a scrim literal that stayed
  // light. Neither is a thing a token test can see.
  { name: 'settings-light', page: 'light.html', w: 1440, h: 900, click: 'Settings' },
  // The wake word section, where the selectable tiles live. Their titles
  // were painted in the console's hairline colour, so on the dark theme
  // they were invisible — and no shot went that far down. Shot in GERMAN,
  // because English is the source and cannot be wrong: what this has to
  // catch is a string that never left it.
  { name: 'settings-tiles', page: 'dark.html', w: 1440, h: 900,
    click: 'Einstellungen', lang: 'de', scroll: 700 },
  // The click target is a LABEL, so it is language-dependent — which is
  // itself worth shooting: if the German build ever stopped translating the
  // header, this shot would fail rather than quietly photograph English.
  { name: 'settings-de', page: 'dark.html', w: 1440, h: 1200, click: 'Einstellungen', lang: 'de' },
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
  const { density = 'roomy', lang = 'en', theme, open, click, setup, scroll } = shot;
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
  if (scroll) await scrollModal(page, scroll);
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
