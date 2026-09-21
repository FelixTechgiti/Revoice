#!/bin/bash
# Build the three DNS-proxy instruments for the Echo Dot.
#
# `dnsprobe` and `fakeproxy` link NO libc — raw ARM EABI syscalls, a few KB —
# for mdnsprobe's reasons: they cross the controller's shell plane as base64,
# and the libc under test is the thing being measured, so depending on none
# removes the question instead of answering it.
#
# `gaiprobe.so` is the opposite and has to be: it measures BIONIC's getaddrinfo,
# so it must run inside a fully initialised bionic process. It is LD_PRELOADed
# into one of the device's own 32-bit binaries, and links against a STUB
# `libc.so` that exports nothing but the handful of symbols it imports — the
# real bionic provides them at run time. That avoids needing a copy of the
# device's libc to link against, and the stub never reaches the device.
#
# clang cross-compiles without a toolchain install; arm-linux-gnueabi-gcc works
# too. --hash-style=sysv is not optional: Android 5.1's linker refuses a binary
# with only a GNU hash ("empty/missing DT_HASH").
set -e
cd "$(dirname "$0")"

CC=${CC:-clang}
TARGET=(--target=armv7-unknown-linux-gnueabi -fuse-ld=lld)
if [[ "$CC" != clang* ]]; then TARGET=(); fi
COMMON=(-Os -march=armv7-a -nostdlib -fno-stack-protector -fno-builtin
        -Wl,--hash-style=sysv)

"$CC" "${TARGET[@]}" "${COMMON[@]}" -static -o dnsprobe  dnsprobe.c
"$CC" "${TARGET[@]}" "${COMMON[@]}" -static -o fakeproxy fakeproxy.c
"$CC" "${TARGET[@]}" "${COMMON[@]}" -shared -fPIC -Wl,-soname,libc.so -o libc.so libc_stub.c
"$CC" "${TARGET[@]}" "${COMMON[@]}" -shared -fPIC -Wl,-soname,gaiprobe.so \
      -o gaiprobe.so gaiprobe.c ./libc.so
rm -f libc.so

for f in dnsprobe fakeproxy gaiprobe.so; do
    ${STRIP:-llvm-strip} "$f" 2>/dev/null || true
    echo "built $f — $(stat -c%s "$f") bytes"
done

cat <<'NOTE'

To put one on a device without a cable, through the controller's shell:
  gzip -n -9 -c dnsprobe | base64 -w0
then on the device:
  printf '%s' '<base64>' > f.b64
  busybox base64 -d f.b64 | busybox gunzip > dnsprobe && chmod 755 dnsprobe

ALWAYS compare md5 on both ends before running it, and compare the DECOMPRESSED
file: gzip stamps an mtime into its header (hence -n above), and a single
mistyped base64 character produces a file of exactly the right length.
NOTE
