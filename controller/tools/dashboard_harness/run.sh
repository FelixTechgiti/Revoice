#!/usr/bin/env bash
# Transpile the current dashboard, rebuild the harness pages from the current
# dashboard.html, and shoot. See README.md.
set -euo pipefail
cd "$(dirname "$0")"
[ -d node_modules ] || npm install
node build.mjs
node shot.mjs
