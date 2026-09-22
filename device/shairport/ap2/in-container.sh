#!/bin/bash
# The AirPlay 2 build, as it runs INSIDE the container. See ../build-ap2.sh.
#
# A file rather than a `bash -c` string, unlike the classic recipe. That one
# carries a warning in three places that an apostrophe in a comment ends the
# single-quoted string and breaks the build — a trap that has nothing to teach
# and costs a cycle every time somebody forgets. This script is mounted and
# run, so it can be read, linted and quoted normally.
#
# Everything it fetches is pinned. Where the classic recipe pins a tarball by
# sha256 it is repeated verbatim here; where it cannot, the pin is a git tag,
# which is what the classic recipe already does for mbedtls and libconfig.
#
# It BUILDS, in CI, since 2026-09-17 — four corrections rather than the classic
# recipe's seven, all of them recorded in ../README.md under "What the first
# runs found". What it has never done is RUN: no AirPlay 2 binary from here has
# been started on a Dot. Read the corrections before moving any pin; three of
# the four are autoconf asking a question a cross build cannot answer, and that
# class of trap comes back with every version bump.

set -euo pipefail

SPS_REF="${SPS_REF:-4.3.7}"
NQPTP_REF="${NQPTP_REF:-1.2.8}"

PREFIX=/build/prefix
JOBS="$(nproc)"

mkdir -p "$PREFIX/lib" "$PREFIX/include"
cd /build

say() { echo; echo "=== $* ==="; }

# ---------------------------------------------------------------------------
say "librt.a and libpthread.a — empty, and not a hack to be tidied away"
# configure.ac requires both unconditionally (AC_CHECK_LIB([rt],[clock_gettime])
# and AC_CHECK_LIB([pthread],[pthread_create], each with an AC_MSG_ERROR behind
# it) under with_os=linux, which is what Android is. bionic has NEITHER library:
# both APIs live in libc, where glibc moved them in 2.17 and 2.34. Every distro
# keeps stub archives so exactly these checks keep passing; Android does not, so
# both checks fail on functions the platform HAS.
: > /build/stub.c
"$CC" -c /build/stub.c -o /build/stub.o
llvm-ar rcs "$PREFIX/lib/librt.a" /build/stub.o
llvm-ar rcs "$PREFIX/lib/libpthread.a" /build/stub.o

# ---------------------------------------------------------------------------
say "popt"
# From the release tarball, not the git tag: the tarball ships a generated
# `configure`, while the git tree needs autopoint to build translations that
# --disable-nls then throws away. sha256 is the classic recipe's, unchanged.
#
# ac_cv_header_glob_h=no is the load-bearing part. The NDK ships glob.h but
# declares glob() only for __ANDROID_API__ >= 28, so the header check passes
# and the LINK fails. Everything using it is behind HAVE_GLOB_H.
curl -sSLO http://ftp.rpm.org/popt/releases/popt-1.x/popt-1.19.tar.gz
echo "c25a4838fc8e4c1c8aacb8bd620edb3084a3d63bf8987fdad3ca2758c63240f9  popt-1.19.tar.gz" \
    | sha256sum -c -
tar xzf popt-1.19.tar.gz
(
    cd popt-1.19
    ac_cv_header_glob_h=no ./configure --host=armv7a-linux-androideabi \
        --prefix="$PREFIX" --disable-shared --enable-static --disable-nls
    make -j"$JOBS"
    make install
)

# ---------------------------------------------------------------------------
say "libconfig"
# SUBDIRS=lib builds the library alone; the default target descends into doc/
# and runs makeinfo on a manual nobody on the device can read.
git clone --depth 1 --branch v1.8.2 https://github.com/hyperrealm/libconfig
(
    cd libconfig
    autoreconf -fi
    ./configure --host=armv7a-linux-androideabi --prefix="$PREFIX" \
        --disable-shared --enable-static --disable-cxx --disable-examples
    make SUBDIRS=lib -j"$JOBS"
    make SUBDIRS=lib install
)

# ---------------------------------------------------------------------------
say "mbedtls"
# Still mbedtls for shairport-sync's own crypto, exactly as the classic build.
# It is NOT displaced by libgcrypt below: those are two different consumers.
git clone --depth 1 --branch v2.28.8 https://github.com/Mbed-TLS/mbedtls
cmake -S mbedtls -B mbedtls/build \
    -DCMAKE_TOOLCHAIN_FILE="$NDK_ROOT/build/cmake/android.toolchain.cmake" \
    -DANDROID_ABI=armeabi-v7a -DANDROID_PLATFORM=android-22 \
    -DENABLE_TESTING=OFF -DENABLE_PROGRAMS=OFF \
    -DCMAKE_INSTALL_PREFIX="$PREFIX"
cmake --build mbedtls/build --target install -j"$JOBS"

# ---------------------------------------------------------------------------
say "libgpg-error"
# libgcrypt needs it, and libgcrypt is not optional — see the shairport step.
#
# --host=arm-unknown-linux-androideabi, and NOT the armv7a spelling used
# everywhere else in this file. That is not carelessness and it is the single
# trap in this library:
#
# For a host matching *-gnu*/*-musl*, configure.ac generates the lock object
# header by running objdump over a probe. Android matches neither, so it falls
# to `force_use_syscfg=yes` and src/mkheader.c:621 includes
# `syscfg/lock-obj-pub.$host.h` — a file that must ALREADY EXIST for the exact
# canonicalised triplet. The tree ships lock-obj-pub.arm-unknown-linux-
# androideabi.h; it does not ship an armv7a one, and `armv7a-linux-androideabi`
# canonicalises to armv7a-unknown-linux-androideabi, which would not match.
# CC is unchanged, so the objects are still armv7a.
git clone --depth 1 --branch libgpg-error-1.61 https://github.com/gpg/libgpg-error
(
    cd libgpg-error
    ./autogen.sh
    ./configure --host=arm-unknown-linux-androideabi --prefix="$PREFIX" \
        --disable-shared --enable-static --disable-nls --disable-doc \
        --disable-tests --enable-install-gpg-error-config
    make -j"$JOBS"
    make install
)

# ---------------------------------------------------------------------------
say "libgcrypt"
# NOT a choice, and not a replacement for mbedtls. Makefile.am:5 in
# shairport-sync builds lib_pair_ap.a with a hardcoded -DCONFIG_GCRYPT:
#
#   lib_pair_ap_a_CFLAGS = -Wall -g -DCONFIG_GCRYPT -pthread
#
# pair_ap is the AirPlay 2 pairing and encryption, and pair-internal.h branches
# only on CONFIG_GCRYPT / CONFIG_OPENSSL — there is no mbedTLS path in it at
# all. So an AirPlay 2 build needs libgcrypt IN ADDITION to whatever
# --with-ssl selects.
#
# --disable-asm because the NDK assembler and libgcrypt's ARM assembly have
# historically disagreed, and this is a build nobody can iterate on quickly.
# Turn it back on once the binary runs; it is a performance question and this
# device decodes one AAC stream.
git clone --depth 1 --branch libgcrypt-1.12.4 https://github.com/gpg/libgcrypt
(
    cd libgcrypt
    ./autogen.sh
    ./configure --host=armv7a-linux-androideabi --prefix="$PREFIX" \
        --with-libgpg-error-prefix="$PREFIX" \
        --disable-shared --enable-static --disable-doc --disable-asm \
        --disable-tests
    make -j"$JOBS"
    make install
)

# ---------------------------------------------------------------------------
say "libsodium"
git clone --depth 1 --branch 1.0.22-RELEASE https://github.com/jedisct1/libsodium
(
    cd libsodium
    ./autogen.sh -s
    ./configure --host=armv7a-linux-androideabi --prefix="$PREFIX" \
        --disable-shared --enable-static
    make -j"$JOBS"
    make install
)

# ---------------------------------------------------------------------------
say "libplist"
# shairport-sync accepts >= 2.0.0 and takes a different code path at >= 2.3.0
# (HAVE_LIBPLIST_GE_2_3_0); 2.7.0 is current.
#
# --without-cython: the bindings need Python headers for the TARGET, which do
# not exist here, and nothing in this build imports plist from Python.
git clone --depth 1 --branch 2.7.0 https://github.com/libimobiledevice/libplist
(
    cd libplist
    ./autogen.sh --host=armv7a-linux-androideabi --prefix="$PREFIX" \
        --disable-shared --enable-static --without-cython
    make -j"$JOBS"
    make install
)

# ---------------------------------------------------------------------------
say "ffmpeg (trimmed)"
# The AirPlay 2 dependency nobody can avoid: configure.ac requires libavutil,
# libavcodec, libavformat and libswresample for --with-airplay-2.
#
# What it is actually FOR is one codec. AIRPLAY2.md: Buffered Audio is "AAC
# stereo at 44,100 frames per second" and Realtime streams are ALAC. So this is
# --disable-jni and --disable-mediacodec are NOT covered by
# --disable-everything, and that is the trap: --disable-everything turns off
# COMPONENTS (decoders, muxers, filters), while jni and mediacodec are external
# library bindings that ffmpeg enables BY DEFAULT for --target-os=android. They
# add -landroid -lmediandk to ffmpeg's link requirements, those propagate into
# shairport-sync-ap2, and neither library exists on a FireOS 6 /system:
#
#   CANNOT LINK EXECUTABLE "shairport-sync-ap2": library "libandroid.so" not found
#
# Measured 2026-09-22 on a FireOS 6 device. `readelf -d` on the published
# binary listed libandroid.so and libmediandk.so; the classic receiver next to
# it needs libm, libdl and libc and runs. The AirPlay 2 path wants neither
# binding — every decoder it uses is requested explicitly below and all of them
# are software.
#
# The failure is invisible in the build: nothing here mentions -landroid, the
# link succeeds, the binary strips clean, and it dies at exec on the device.
# The same shape as the CXXLD note below.
# --disable-everything plus the two decoders, which keeps a general-purpose
# media framework from becoming several megabytes on a device that shares
# 512MB with Android.
#
# n7.1.5 rather than the newest: 7.1 is the series every Android NDK recipe in
# the wild is written against, and this build cannot be iterated on cheaply.
git clone --depth 1 --branch n7.1.5 https://github.com/FFmpeg/FFmpeg ffmpeg
(
    cd ffmpeg
    # Stop ffmpeg finding the NDK's media libraries, because no configure flag
    # can. `libandroid.so` and `libmediandk.so` do not exist on a FireOS
    # /system, and a binary that needs them dies at exec with
    #
    #   CANNOT LINK EXECUTABLE "shairport-sync-ap2": library "libandroid.so" not found
    #
    # `--disable-android` and `--disable-mediandk` are not the answer and were
    # tried (#297): neither name is in any of configure's *_LIST variables, so
    # they are not even valid options, and the detection is UNCONDITIONAL --
    # two bare `check_lib` calls that run whatever the command line said:
    #
    #   check_lib android  android/native_window.h ANativeWindow_acquire -landroid
    #   check_lib mediandk "stdint.h media/NdkMediaFormat.h" AMediaFormat_new -lmediandk
    #
    # and `check_lib` opens with `disable $name` before re-enabling on success,
    # so an explicit disable would be overwritten by the probe even if it could
    # be expressed. Deleting the two lines is what actually decides it.
    #
    # It cascades correctly rather than leaving a half-configured tree:
    # `mediacodec_deps="android mediandk"`, so mediacodec switches off with
    # them and `libavcodec/mediacodec.o` is never compiled. That matters --
    # patching the .pc files instead would leave those objects in
    # `libavcodec.a` and trade a runtime failure for an undefined symbol.
    #
    # Asserted rather than assumed: the release workflow reads every published
    # binary's NEEDED list against what a FireOS /system carries, which is how
    # the previous attempt was caught before it reached a device.
    # Checked BEFORE the edit, not after: a future ffmpeg that renames or moves
    # these probes would let `sed` match nothing, succeed, and quietly restore
    # the bug. The count is the assertion; the deletion is the easy part.
    found=$(grep -cE '^check_lib (android|mediandk) ' configure)
    if [ "$found" != 2 ]; then
        echo "ffmpeg configure has $found of the 2 expected NDK probes — the patch below no longer matches, and the binary would ship needing libandroid.so again (#297)" >&2
        exit 1
    fi
    sed -i '/^check_lib android /d; /^check_lib mediandk /d' configure
    ./configure \
        --prefix="$PREFIX" \
        --enable-cross-compile \
        --cross-prefix=llvm- \
        --cc="$CC" \
        --ar=llvm-ar --ranlib=llvm-ranlib --strip=llvm-strip --nm=llvm-nm \
        --target-os=android \
        --arch=arm \
        --cpu=armv7-a \
        --enable-static --disable-shared \
        --disable-everything \
        --enable-decoder=aac --enable-decoder=aac_latm --enable-decoder=alac \
        --enable-parser=aac --enable-parser=aac_latm \
        --enable-demuxer=aac --enable-demuxer=mov \
        --enable-protocol=file \
        --disable-programs --disable-doc --disable-htmlpages --disable-manpages \
        --disable-avdevice --disable-avfilter --disable-swscale --disable-postproc \
        --disable-network --disable-iconv --disable-symver \
        --disable-debug \
        --disable-jni --disable-mediacodec
    make -j"$JOBS"
    make install
)

# ---------------------------------------------------------------------------
say "the compat shims"
# Six now, where the classic build has two. android_shm.c is the nqptp<->
# shairport clock interface (bionic has no POSIX shared memory); android_uuid.c
# is the three libuuid functions shairport calls once, which util-linux would
# otherwise have to be cross-built for; android_localhost.c is the one call both
# AirPlay 2 binaries make before they can do anything at all, and the one that
# had them exiting once a minute on a real device (#218); tinysvc_txt.c is the
# TXT string encoder tinysvcmdns should have had, and without it the responder
# thread dereferences a NULL on its first announcement because AirPlay 2's
# `pk=` record is longer than a DNS name label may be (#229); ap2_ports.c is
# the port range an AirPlay 2 session's sockets are taken from, so the firewall
# can name them (#79) — without it a session negotiates and then carries no
# audio on any device with a default-DROP policy, which is all of them.
"$CC" -c -O2 -fPIC -I/compat /compat/android_compat.c   -o /build/android_compat.o
"$CC" -c -O2 -fPIC -I/compat /compat/android_ifaddrs.c  -o /build/android_ifaddrs.o
"$CC" -c -O2 -fPIC -I/compat /compat/android_shm.c      -o /build/android_shm.o
"$CC" -c -O2 -fPIC -I/compat /compat/android_uuid.c     -o /build/android_uuid.o
"$CC" -c -O2 -fPIC -I/compat /compat/android_localhost.c -o /build/android_localhost.o
"$CC" -c -O2 -fPIC -I/compat /compat/mdns_ap2.c         -o /build/mdns_ap2.o
"$CC" -c -O2 -fPIC -I/compat /compat/tinysvc_txt.c      -o /build/tinysvc_txt.o
"$CC" -c -O2 -fPIC -I/compat /compat/ap2_ports.c        -o /build/ap2_ports.o
llvm-ar rcs "$PREFIX/lib/libemcompat.a" \
    /build/android_compat.o /build/android_ifaddrs.o /build/android_shm.o \
    /build/android_uuid.o /build/android_localhost.o /build/mdns_ap2.o \
    /build/tinysvc_txt.o /build/ap2_ports.o

# `-luuid` has to resolve, because configure probes for it by name. The symbols
# are in libemcompat.a; this archive is what makes the -l work, the same trick
# as librt and libpthread above.
llvm-ar rcs "$PREFIX/lib/libuuid.a" /build/android_uuid.o

# ---------------------------------------------------------------------------
say "nqptp $NQPTP_REF"
# The PTP clock daemon AirPlay 2 times against. A SECOND process on the device,
# supervised beside shairport-sync, needing UDP 319 and 320 exclusively and the
# privilege to bind them — both of which a rooted Echo has, and neither of
# which needs hardware timestamping: nqptp's own README says it "does not take
# advantage of hardware timestamping", which is the correction at the centre
# of #79.
#
# The shims are injected with -include, so no upstream source is patched.
#
# BOTH headers, and the second one was added after a measurement rather than
# from reading. android_shm.h used to say cancellation was deliberately kept
# out of nqptp's build because it is "a daemon that does not use threads" —
# and nqptp's own debug.c calls pthread_setcancelstate four times, around the
# critical section of every log line. bionic has no cancellation at any API
# level, so the build failed with `use of undeclared identifier
# PTHREAD_CANCEL_DISABLE` (measured in CI, 2026-09-15).
#
# Only the DECLARATIONS were missing: libemcompat.a already carries
# android_compat.o and nqptp already links it, so this adds no code to the
# binary that was not being linked in anyway. It is the real deferred-
# cancellation shim rather than a no-op stub, which matters even though nqptp
# is unlikely to cancel anything: a stub would be a second, weaker
# implementation of something already implemented correctly twenty lines away.
git clone --depth 1 --branch "$NQPTP_REF" https://github.com/mikebrady/nqptp
(
    cd nqptp
    autoreconf -fi
    # ac_cv_func_malloc_0_nonnull is the autoconf cross-compile trap, and it
    # fails at LINK rather than at configure, which is what makes it worth a
    # comment. AC_FUNC_MALLOC decides whether malloc(0) returns non-NULL by
    # RUNNING a program; cross-compiling it cannot, so it assumes broken and
    # emits `#define malloc rpl_malloc` — a replacement nobody provides:
    #
    #     nqptp.o: undefined reference to 'rpl_malloc'
    #
    # Measured in CI 2026-09-15. bionic's malloc(0) returns a unique non-NULL
    # pointer like every other modern libc, so answering the question the
    # test could not ask is a statement of fact rather than a workaround.
    # realloc carries the identical trap and is answered beside it, because
    # the two differ only in which source file happens to call the other one.
    ./configure --host=armv7a-linux-androideabi --prefix="$PREFIX" \
        ac_cv_func_malloc_0_nonnull=yes \
        ac_cv_func_realloc_0_nonnull=yes \
        CFLAGS="-O2 -I$PREFIX/include -include /compat/android_shm.h" \
        LDFLAGS="-L$PREFIX/lib -static-libgcc" \
        LIBS="-lemcompat"
    # The cancellation shim goes to MAKE, not to configure, and that split is
    # forced rather than chosen. android_compat.h includes <pthread.h>;
    # autoconf's AC_CHECK_LIB declares the function it is probing for itself,
    # as `char pthread_create ();`, and a real prototype in scope makes that
    # conflict — so the test program fails to COMPILE and configure reports it
    # as a missing library:
    #
    #     checking for pthread_create in -lpthread... no
    #     configure: error: pthread library needed
    #
    # Measured in CI 2026-09-15, one run after the shim was added. android_shm.h
    # can stay above because it includes only <stddef.h> and <sys/types.h>, and
    # it has to: configure's own feature tests must see the shm rename.
    #
    # AM_CFLAGS survives this — `-fno-common -Wall -Wextra -pthread
    # --include=config.h` come from the Makefile, not from here.
    #
    # android_localhost.h is the third, and it is the one without which nqptp
    # builds perfectly and exits one second after every start: nqptp.c:281 binds
    # its control port by the NAME `localhost`, and on this device nothing
    # answers for it (#218, #219). It goes at make time with the others.
    make -j"$JOBS" CXXLD="$CC" \
        CFLAGS="-O2 -I$PREFIX/include -include /compat/android_shm.h -include /compat/android_compat.h -include /compat/android_localhost.h"
)

# ---------------------------------------------------------------------------
say "shairport-sync $SPS_REF, with AirPlay 2"
git clone --depth 1 --branch "$SPS_REF" https://github.com/mikebrady/shairport-sync
cd shairport-sync

# OUR mDNS backend replaces upstream's, and it is a whole file rather than a
# patch: --with-tinysvcmdns already compiles this filename, so configure.ac,
# Makefile.am and mdns.c are untouched and the 4.3.7 pin stays movable.
#
# Without it this build would configure, compile, link, run — and never once be
# offered to a phone as an AirPlay 2 device, because upstream's version takes
# ap2name and secondary_txt_records and declares both
# __attribute__((unused)). See compat/mdns_tinysvcmdns.c.
cp /compat/mdns_tinysvcmdns.c mdns_tinysvcmdns.c

# Take AirPlay 2's per-session sockets from a range a firewall rule can name
# (#79). Unlike the mDNS backend this is a PATCH and not a whole file: common.c
# is large, central and nothing to do with us, so owning a copy of it would
# mean re-reading upstream's diff on every bump to keep a file we did not want.
#
# The edit is a rename, so the original function is preserved byte for byte and
# only answers to a different name; the wrapper appended below is the whole of
# what we add. Every one of the four sites that asks for "any port" —
# local_event_port twice, local_ap2_control_port and local_buffered_audio_port
# — goes through this one function, which is why the patch is one line.
#
# Guarded, because a silent no-op here ships a binary that looks right and
# stalls every session: the rename either matches exactly one definition or the
# build stops. The signature is checked rather than the name alone, since a
# future common.c could plausibly keep the name and change the arguments — and
# then the wrapper would not compile, which is the failure we want, but the
# message would be about a type rather than about this patch.
found=$(grep -cE '^int bind_socket_and_port\(int type, int ip_family, const char \*self_ip_address, uint32_t scope_id,$' common.c)
if [ "$found" != 1 ]; then
    echo "common.c has $found definitions matching the expected bind_socket_and_port signature, not 1 — the patch below no longer matches, and AirPlay 2 would ship taking kernel-chosen ports again (#79)" >&2
    exit 1
fi
sed -i 's/^int bind_socket_and_port(int type,/int em_bind_socket_and_port_once(int type,/' common.c
cat /compat/ap2_ports_wrapper.c >> common.c

autoreconf -fi

BASE_CFLAGS="-I$PREFIX/include -I/compat -O2"

# --with-airplay-2 is the flag; everything else is the same shape as the
# classic build. --without-pkg-config keeps the HOST's .pc files out of a cross
# build, where they hand back host include and library paths that link an x86
# object into an ARM binary — a failure that arrives at the linker with no
# mention of architecture. PKG_CONFIG_LIBDIR confines it as well; neither alone
# is trusted.
#
# LIBS order is a static-link order. Each entry resolves symbols the entries
# before it refer to: mbedtls calls into its siblings, libgcrypt into
# libgpg-error, the ffmpeg trio into libavutil, and -lemcompat last because
# nothing in it needs anything further along.
PKG_CONFIG_LIBDIR="$PREFIX/lib/pkgconfig" \
./configure --host=armv7a-linux-androideabi \
    --with-airplay-2 \
    --with-stdout --with-tinysvcmdns --with-ssl=mbedtls --with-metadata \
    --without-pkg-config \
    --without-alsa --without-pa --without-pw --without-soxr \
    CFLAGS="$BASE_CFLAGS" \
    LDFLAGS="-L$PREFIX/lib -static-libgcc" \
    LIBS="-lmbedx509 -lmbedcrypto -lgcrypt -lgpg-error -lsodium -lplist-2.0 \
          -lavformat -lavcodec -lswresample -lavutil -luuid -lemcompat"

# The shim is added at MAKE time, not at configure time, and that is the
# difference between configuring and not: autoconf probes a libc function by
# declaring it itself as `char clock_gettime();`, and the shim pulls in
# <time.h>, which declares the real prototype. Every check then fails
# identically and the first to report it is `librt needed` — a message pointing
# at the stub archive above, which is perfectly fine.
#
# CXXLD is the other half. configure runs AC_PROG_CXX so automake links with
# clang++, which pulls in libc++_shared.so — a C++ runtime that is not on
# FireOS 5 and never will be. It builds clean, strips clean, and dies at exec.
# BOTH shims, and android_shm.h is the one that is easy to leave out here:
# nothing in shairport-sync's configure probes for shared memory, so its
# absence costs nothing until the LINK, where ptp-utilities.c:176 asks for
# shm_open — the single call that reads the clock nqptp publishes, and the one
# function on the whole AirPlay 2 path bionic does not have. Without the header
# the call compiles (implicit declaration, a warning among thousands) and the
# link fails on a name that looks like a missing library rather than a missing
# shim. It goes at MAKE time for the same reason as below, and it could have
# gone at configure time as it does for nqptp; there it had to, because
# nqptp's own feature tests see the rename.
#
# android_localhost.h is the third and is needed for the same reason at the
# other end of the same socket: ptp-utilities.c:239 resolves `localhost` to SEND
# nqptp a control message, and dies if it cannot (#218). Note both binaries need
# it — the one without it is the one that fails, and the failures look like two
# unrelated faults in a log.
make -j"$JOBS" \
    CFLAGS="$BASE_CFLAGS -include /compat/android_shm.h -include /compat/android_compat.h -include /compat/android_localhost.h" \
    CXXLD="$CC"

# ---------------------------------------------------------------------------
say "checking both binaries before they leave the container"
cd /build

check_elf() {
    local path="$1" name="$2"
    if "$NDK/bin/llvm-readelf" -d "$path" | grep -q "libc++"; then
        echo "ERROR: $name needs a C++ runtime the device does not have" >&2
        "$NDK/bin/llvm-readelf" -d "$path" | grep NEEDED >&2
        exit 1
    fi
    # Proof rather than assumption: the whole hazard is that everything looks
    # fine until the device refuses the file.
    local arch
    arch="$("$NDK/bin/llvm-readelf" -h "$path" | awk -F: '/Machine/ {print $2}')"
    case "$arch" in
        *ARM*) ;;
        *) echo "ERROR: $name is not an ARM binary:$arch" >&2; exit 1 ;;
    esac
    echo "$name:"
    "$NDK/bin/llvm-readelf" -d "$path" | grep NEEDED || true
}

check_elf /build/shairport-sync/shairport-sync shairport-sync
check_elf /build/nqptp/nqptp nqptp

# The version token the firmware gates on. internal/airplay matches
# `(^|-)AirPlay2(-|$)` in `shairport-sync -V` and starts the clock daemon only
# on a hit; common.c:1807 appends that token only under CONFIG_AIRPLAY_2. An
# armv7a binary cannot be RUN here, but the token is a string literal in
# .rodata, so finding it is the same fact.
#
# Without this the failure is silent in the worst direction: configure would
# have to fall back to a classic build for some reason nobody read in three
# hundred lines of output, the script would report success, the receiver would
# serve classic AirPlay perfectly, and nqptp would sit beside it never started.
# That is the one feature this whole recipe exists for, disappearing without a
# single error.
if ! grep -aq -- "-AirPlay2" /build/shairport-sync/shairport-sync; then
    echo "ERROR: the binary carries no -AirPlay2 token — this is a CLASSIC" >&2
    echo "       build, and the device would never start the clock daemon." >&2
    exit 1
fi
echo "shairport-sync reports AirPlay 2"

# The shared-memory ABI, which is the coupling that is silent at BOTH ends.
# shairport stamps its own NQPTP_SHM_STRUCTURES_VERSION into the version
# string as `-smi<N>`; nqptp writes its own into every record. They are two
# copies of one number in two separately pinned trees, and a disagreement is
# not an error anywhere — the reader simply never accepts a record, so AirPlay
# 2 plays out of sync with nothing logged. Both pins are moved by hand, so the
# check belongs where they are read rather than where they were written.
sps_smi="$(sed -n 's/^#define NQPTP_SHM_STRUCTURES_VERSION *\([0-9]*\).*/\1/p' \
    /build/shairport-sync/nqptp-shm-structures.h)"
nq_smi="$(sed -n 's/^#define NQPTP_SHM_STRUCTURES_VERSION *\([0-9]*\).*/\1/p' \
    /build/nqptp/nqptp-shm-structures.h)"
if [ -z "$sps_smi" ] || [ -z "$nq_smi" ]; then
    echo "ERROR: could not read the shared-memory ABI version from both trees" >&2
    echo "       (shairport='$sps_smi' nqptp='$nq_smi')" >&2
    exit 1
fi
if [ "$sps_smi" != "$nq_smi" ]; then
    echo "ERROR: shairport-sync $SPS_REF expects shm ABI $sps_smi and nqptp" >&2
    echo "       $NQPTP_REF publishes $nq_smi. They would not interoperate," >&2
    echo "       and neither of them would say so." >&2
    exit 1
fi
echo "shared-memory ABI: both at version $sps_smi"

# The loopback shim, in BOTH binaries, and this is checked rather than assumed
# because the -include that carries it is one word in one make line per program
# and its absence is invisible until a device is in front of you (#218).
#
# Presence is proof rather than a hint, and the static link is why: libemcompat.a
# is an archive, so ld pulls android_localhost.o in only if something REFERENCES
# a symbol it defines. The symbol being there therefore says the rename reached a
# call site, which is the thing that can be missing. It has to run before
# llvm-strip below, which takes the symbol table with it.
#
# **llvm-readelf, never llvm-nm.** This cost two release runs. `llvm-nm` in the
# pinned image reads these binaries as five debug entries with EMPTY names:
#
#     00000000 N
#     00000000 N
#
# — no error, exit 0, and a name-based test on that output can only ever fail.
# llvm-readelf is the tool the checks above already use on the same two files,
# so it is the one with evidence behind it rather than the one that reads
# right. The general form: a check is only as trustworthy as the tool under it,
# and a tool that answers confidently with nothing is worse than one that errors.
#
# The whole output is captured ONCE into a variable and matched with a case
# glob. The first diagnostic version piped into `head -5`, which SIGPIPEs the
# producer under pipefail and killed the script mid-report — so the run that was
# supposed to explain itself printed its first five lines and exited 74, with the
# comparison that mattered never reached.
shim_check() {
    local binary="$1" obj="$2" syms objsyms
    syms="$("$NDK/bin/llvm-readelf" --symbols "$binary" 2>&1 || true)"
    case "$syms" in
        *em_getaddrinfo*) return 0 ;;
    esac
    echo "ERROR: $binary was linked without the loopback shim." >&2
    echo "       It resolves \`localhost\` to reach nqptp's control port," >&2
    echo "       which no resolver on the device answers — so it would" >&2
    echo "       build, install, and exit once a minute for ever." >&2
    # em_shm_open is in every one of these binaries by construction — without it
    # there would be no binary to check. So this line separates "the reader is
    # not reading" from "the shim is really missing".
    echo "--- the other shims, which are known to be linked:" >&2
    printf '%s\n' "$syms" | grep " em_" >&2 || echo "    (no em_ symbol at all — suspect the reader)" >&2
    # shellcheck disable=SC2086
    objsyms="$("$NDK/bin/llvm-readelf" --symbols $obj 2>&1 || true)"
    echo "--- addrinfo in the object holding the call site:" >&2
    printf '%s\n' "$objsyms" | grep -i addrinfo >&2 || echo "    (none — the -include did not reach it)" >&2
    exit 1
}
shim_check /build/shairport-sync/shairport-sync "/build/shairport-sync/*ptp-utilities.o"
shim_check /build/nqptp/nqptp "/build/nqptp/*utilities.o"
echo "both binaries carry the loopback shim"

# The TXT encoder, in shairport-sync only — nqptp advertises nothing. Same
# argument as above and the same reader: libemcompat.a is an archive, so
# em_txt_label is in the binary only if a call site references it, and the call
# site is one line in our own mdns_tinysvcmdns.c. Reverting that line to pass
# the strings to mdnsd_register_svc builds, links, installs and then kills the
# responder thread on its first announcement, because AirPlay 2's `pk=` record
# is 67 bytes and tinysvcmdns runs TXT strings through the 63-byte name-label
# helper (#229). Nothing else would report it; the symbol does.
txtsyms="$("$NDK/bin/llvm-readelf" --symbols /build/shairport-sync/shairport-sync 2>&1 || true)"
case "$txtsyms" in
    *em_txt_label*) echo "shairport-sync carries the TXT encoder" ;;
    *)
        echo "ERROR: shairport-sync was linked without em_txt_label." >&2
        echo "       Its mDNS responder would dereference a NULL encoding the" >&2
        echo "       67-byte \`pk=\` record, about a second after every start." >&2
        echo "--- the em_ symbols it does carry:" >&2
        printf '%s\n' "$txtsyms" | grep " em_" >&2 || echo "    (none at all — suspect the reader)" >&2
        exit 1
        ;;
esac

"$NDK/bin/llvm-strip" /build/shairport-sync/shairport-sync
"$NDK/bin/llvm-strip" /build/nqptp/nqptp

cp /build/shairport-sync/shairport-sync /out/shairport-sync-ap2
cp /build/nqptp/nqptp /out/nqptp
( cd /out && md5sum shairport-sync-ap2 nqptp > ap2.md5 )

say "done"
ls -lh /out/shairport-sync-ap2 /out/nqptp
