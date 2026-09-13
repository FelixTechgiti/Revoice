# Looking at the dashboard without a controller

The dashboard is the one part of this project nobody could see without a
running controller and a fleet of real Echo Dots — so every change to it
shipped on reasoning rather than on looking, and "not verified: how it looks"
was the honest note on every commit that touched it.

This renders `static/dashboard.jsx` in a headless browser against fixed
device fixtures. It is a development tool, not a test: it has no assertions
and CI does not run it. What it gives you is a PNG of each theme, each
density and a narrow viewport, plus every `pageerror` and console error the
page raised on the way — which is how a component that throws at render is
found, since React logs it and shows a blank page.

## Running it

```bash
cd controller/tools/dashboard_harness
npm install                       # puppeteer-core, nothing else
./run.sh                          # or: node build.mjs && node shot.mjs
```

Screenshots land in `out/`. `run.sh` transpiles the current
`static/dashboard.jsx`, rebuilds the harness pages from the current
`static/dashboard.html` (so the tokens and chrome classes under test are the
real ones), serves them, and shoots.

**It needs a Chromium.** `CHROME=/path/to/chrome ./run.sh` if the usual
places do not have one — the script names what it looked for when it cannot
find one.

## What is real and what is not

Real: `dashboard.jsx` exactly as it ships, `dashboard.html`'s whole `<style>`
block, and React at the pinned version.

Faked, in `boot.js`: `fetch` answers the handful of endpoints the fleet page
loads, `WebSocket` is a stub so the poll fallback carries the page, and xterm
is a stub because the console tab would otherwise need the real library. The
fonts come from Google rather than `static/vendor/fonts/`, which the
controller image fetches at build time and a checkout does not have.

So this shows LAYOUT and COLOUR. It does not show anything that depends on a
device answering, and a screenshot from it is not evidence that a control
works.

## The fixtures

One device listening, one playing over Spotify, one offline for fourteen
minutes on older firmware, one waiting for approval. Chosen so that every
state the fleet page can render appears at once — including the ones that
are easy to leave broken because they are rare: an unnamed device, a null
volume, a missing latency, the "source known, title unknown" playback line.

Edit `boot.js` to look at something else.
