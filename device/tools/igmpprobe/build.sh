#!/bin/bash
# Build igmpprobe for the Echo Dot.
#
# Freestanding, exactly as mdnsprobe is and for the same reason: it reaches a
# device as base64 through the controller's shell plane, so size is the whole
# constraint, and linking no libc removes the bionic question instead of
# answering it.
set -e

CC=${CC:-arm-linux-gnueabi-gcc}
STRIP=${STRIP:-arm-linux-gnueabi-strip}
OUT=${1:-igmpprobe}

command -v "$CC" >/dev/null || {
    echo "No $CC. On Debian/Ubuntu:" >&2
    echo "  apt-get install gcc-arm-linux-gnueabi libc6-dev-armel-cross" >&2
    exit 1
}

# -mfloat-abi=soft because no float ever crosses a boundary here and the
# hard-float cross-compilers refuse -march=armv7-a without an -mfpu; this
# keeps one command line working on both.
"$CC" -Os -march=armv7-a -mfloat-abi=soft -nostdlib -static -fno-stack-protector -fno-builtin \
      -o "$OUT" "$(dirname "$0")/igmpprobe.c"
"$STRIP" "$OUT" 2>/dev/null || true

echo "built $OUT — $(stat -c%s "$OUT") bytes"
echo
echo "To put it on a device without a cable, through the controller's shell:"
echo "  gzip -n -9 -c $OUT | base64 -w0"
echo "then on the device:"
echo "  echo -n '<base64>' > f.b64"
echo "  busybox base64 -d f.b64 | busybox gunzip > probe && chmod 755 probe"
echo
echo "Compare md5 on both ends before running it, and note -n above: gzip"
echo "stamps an mtime into its header, so two runs of one source differ."
