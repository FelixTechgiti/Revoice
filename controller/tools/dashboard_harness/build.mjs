// Build the harness pages from the CURRENT dashboard sources.
//
// The style block is copied out of dashboard.html rather than maintained
// here, because a second copy of the tokens is a second answer to what the
// colours are — and the whole point of looking at this is to see the ones
// that ship.
import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';
import * as esbuild from 'esbuild';

const HERE = dirname(fileURLToPath(import.meta.url));
const STATIC = join(HERE, '..', '..', 'static');
const OUT = join(HERE, 'out');
if (!existsSync(OUT)) mkdirSync(OUT, { recursive: true });

// Same settings the image build uses (controller/Dockerfile). Through the
// JS API rather than the binary: the .bin shim on Windows is a .cmd, which
// execFileSync refuses with EINVAL.
await esbuild.build({
  entryPoints: [join(STATIC, 'dashboard.jsx')],
  bundle: false,
  jsx: 'transform',
  jsxFactory: 'React.createElement',
  jsxFragment: 'React.Fragment',
  outfile: join(OUT, 'dashboard.js'),
  logLevel: 'info',
});

const html = readFileSync(join(STATIC, 'dashboard.html'), 'utf8');
let style = html.slice(html.indexOf('<style>'), html.indexOf('</style>') + 8);
// The vendored woff2 files are fetched into the image at build time and are
// not in a checkout, so the faces come from Google here instead. Same
// families, same weights.
style = style.replace(/\n {4}@font-face \{[^\n]*\}/g, '');

const FONTS = `  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Instrument+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500&display=swap">
`;

// xterm is stubbed rather than vendored: the console tab is the only thing
// that wants it, and pulling the real library in would make this harness
// need the same vendor step the image has.
const STUBS = `  <script>
    window.Terminal = function () {
      this.loadAddon = function () {}; this.open = function () {};
      this.focus = function () {}; this.write = function () {};
      this.onData = function () { return { dispose() {} }; };
      this.onResize = function () { return { dispose() {} }; };
      this.dispose = function () {};
    };
    window.FitAddon = { FitAddon: function () { this.fit = function () {}; } };
  </script>
`;

for (const theme of ['dark', 'light']) {
  writeFileSync(join(OUT, `${theme}.html`), `<!DOCTYPE html>
<html lang="en" data-theme="${theme}">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
  <!-- The real page declares this, and without it Chrome asks for
       /favicon.ico and the miss is reported as a page error. -->
  <link rel="icon" type="image/svg+xml" href="static/favicon.svg"/>
  <title>Revoice dashboard harness — ${theme}</title>
${FONTS}${style}
</head>
<body>
  <div id="root"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/react/18.2.0/umd/react.production.min.js"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/react-dom/18.2.0/umd/react-dom.production.min.js"></script>
${STUBS}  <script src="../boot.js"></script>
  <script src="dashboard.js"></script>
</body>
</html>
`);
}
console.log('harness pages written to out/');
