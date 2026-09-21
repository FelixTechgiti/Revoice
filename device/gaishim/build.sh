#!/bin/bash
# Build gaishim.so for the Echo Dot, and check it three ways first.
#
# The checks run BEFORE the build and a failure stops it, because everything
# this library can get wrong is silent on the device: a swapped struct field, a
# host-order port, a leaked list — each produces a program that starts, runs
# and never connects. That is the same reason emos/init's off-target tools
# exist, and this follows their shape.
#
#   gaishimcheck.c  drives the real getaddrinfo on the build host
#   abicheck.c      pins every constant gaishim.c hardcodes against Android's
#                   own headers, for the real target
#   --no-undefined  refuses a library that imports a symbol bionic lacks,
#                   which would otherwise present as a preload that silently
#                   fails to load and an endpoint that behaves as before
#
# Needs an NDK: ANDROID_NDK_HOME, ANDROID_NDK_ROOT, ANDROID_NDK_LATEST_HOME or
# NDK, whichever is set. API 22 is Android 5.1, which is what biscuit runs, and
# it is deliberate that the library is compiled against the same bionic it will
# be loaded into.
#
# --hash-style=sysv is not optional: Android 5.1's linker refuses a library
# with only a GNU hash ("empty/missing DT_HASH").
set -euo pipefail
cd "$(dirname "$0")"

HOSTCC=${HOSTCC:-cc}
echo "== off-target checks (host) =="
"$HOSTCC" -O2 -Wall -Wextra -o "${TMPDIR:-/tmp}/gaishimcheck" gaishimcheck.c
"${TMPDIR:-/tmp}/gaishimcheck"

NDK="${NDK:-${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-${ANDROID_NDK_LATEST_HOME:-}}}}"
if [[ -z "$NDK" ]]; then
    for d in "$ANDROID_HOME"/ndk/* "$ANDROID_SDK_ROOT"/ndk/* \
             /opt/android/ndk/* \
             /opt/homebrew/share/android-commandlinetools/ndk/* \
             /usr/local/lib/android/sdk/ndk/*; do
        [[ -d "$d" ]] && NDK="$d"
    done
fi
if [[ -z "$NDK" || ! -d "$NDK" ]]; then
    echo "No NDK found. Set ANDROID_NDK_HOME (or NDK) to an r21+ NDK." >&2
    exit 1
fi
CC=$(echo "$NDK"/toolchains/llvm/prebuilt/*/bin/armv7a-linux-androideabi22-clang)
STRIP=$(echo "$NDK"/toolchains/llvm/prebuilt/*/bin/llvm-strip)
[[ -x "$CC" ]] || { echo "No armv7a API-22 clang under $NDK" >&2; exit 1; }

echo "== ABI check (armv7a, Android API 22 headers) =="
"$CC" -c -o /dev/null abicheck.c

echo "== armv7a build =="
mkdir -p out
"$CC" -shared -fPIC -Os -Wall -Wextra -fvisibility=hidden \
      -Wl,--no-undefined -Wl,--hash-style=sysv -Wl,-soname,gaishim.so \
      -o out/gaishim.so gaishim.c
"$STRIP" out/gaishim.so

# The library is built -fvisibility=hidden, so the two attributes in gaishim.c
# are the entire export list. Getting that wrong produces a library that loads
# cleanly, exports nothing, interposes nothing, and leaves every endpoint
# behaving exactly as before — with no message anywhere. Measured once, on
# 2026-09-21, which is why it is checked rather than trusted.
NM=$(echo "$NDK"/toolchains/llvm/prebuilt/*/bin/llvm-nm)
exported=$("$NM" --dynamic --defined-only out/gaishim.so | awk '{print $NF}')
for sym in getaddrinfo freeaddrinfo; do
    grep -qx "$sym" <<<"$exported" || {
        echo "out/gaishim.so does not export $sym — it would interpose nothing" >&2
        exit 1
    }
done
echo "exports: $(tr '\n' ' ' <<<"$exported")"

echo "built out/gaishim.so — $(wc -c < out/gaishim.so | tr -d ' ') bytes"
