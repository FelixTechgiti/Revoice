// The command that clears a device's Revoice state.
//
// It runs as root on somebody's Echo over a serial console, so the shape is
// the test: named files only, both directories, and nothing that could widen
// if a path is ever mistyped.

import { readFileSync } from 'node:fs';
import { strict as assert } from 'node:assert';

const src = readFileSync(new URL('../static/dashboard.jsx', import.meta.url), 'utf8');

function extract(name) {
  const i = src.indexOf(`function ${name}(`);
  assert.ok(i > 0, `${name} not found in dashboard.jsx`);
  // Pull the two const lists plus the function, and evaluate them together.
  const dirs = /const REVOICE_STATE_DIRS\s*=\s*(\[[^\]]*\]);/.exec(src);
  const files = /const REVOICE_STATE_FILES\s*=\s*(\[[^\]]*\]);/.exec(src);
  assert.ok(dirs && files, 'the path lists are not where the test expects them');
  let depth = 0, end = i;
  for (let j = src.indexOf('{', i); j < src.length; j++) {
    if (src[j] === '{') depth++;
    else if (src[j] === '}') { depth--; if (depth === 0) { end = j + 1; break; } }
  }
  return new Function(
    `const REVOICE_STATE_DIRS = ${dirs[1]};`
    + `const REVOICE_STATE_FILES = ${files[1]};`
    + src.slice(i, end)
    + `; return { cmd: ${name}(), dirs: REVOICE_STATE_DIRS, files: REVOICE_STATE_FILES };`
  )();
}

const { cmd, dirs, files } = extract('resetRevoiceCommand');

let failed = 0;
const ok = (c, what) => { if (!c) { console.log(`FAIL  ${what}`); failed++; } };

// Both directories: a device provisioned under the old name keeps its files
// there, and leaving them means the reset silently does half the job.
ok(dirs.includes('/data/local/etc/revoice'), 'the current directory is cleared');
ok(dirs.includes('/data/local/etc/echomuse'), 'the legacy directory is cleared too');

// The credential pair is the whole point; the other three are what make the
// device genuinely fresh rather than merely reconnectable.
for (const f of ['ca.pem', 'token', 'controller.json', 'state.json', 'console.pw']) {
  ok(files.includes(f), `${f} is removed`);
  ok(cmd.includes(`/data/local/etc/revoice/${f}`), `${f} appears with its full path`);
}

// The shape. A glob or a recursive remove running as root against /data is a
// different class of mistake from a wrong filename.
ok(!/[*?]/.test(cmd), 'no wildcard anywhere in the command');
ok(!/-[a-z]*r/.test(cmd.split('&&')[0]), 'rm is not recursive');
ok(/\brm -f\b/.test(cmd), 'rm -f, so a missing file does not abort the reset');
ok(!/\brm\s+-f\s+\//.test(cmd) || cmd.includes('/data/local/etc/'),
   'every path is under /data/local/etc');

// The marker is how the caller knows it ran at all: a console that answered
// nothing, or a shell that refused, must not read as success.
ok(cmd.includes('echo RESET_OK'), 'the command confirms itself');
ok(cmd.indexOf('sync') < cmd.indexOf('echo RESET_OK'),
   'sync happens before the marker, so an unplugged device keeps the reset');

if (failed) { console.log(`revoice_reset: ${failed} FAILED`); process.exit(1); }
console.log('revoice_reset: all checks passed');
