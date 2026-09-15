#!/bin/bash
# Build shairport-sync WITH AIRPLAY 2, plus nqptp, for the Echo Dot.
#
# Separate from build.sh rather than a mode inside it. The two recipes share
# four library builds and differ in everything else — a different SSL
# arrangement, six more dependencies, a replaced mDNS backend and a second
# binary — and build.sh is the path currently being proven on hardware (#16).
# A mode flag would put a branch into a script nobody can run in CI, on the one
# file whose failure mode is "the device refuses to exec it".
#
# Needs a machine with a Docker daemon. Claude Code cloud sessions have none.
#
# Usage: ./build-ap2.sh [shairport-ref] [nqptp-ref]
set -euo pipefail

SPS_REF="${1:-4.3.7}"
NQPTP_REF="${2:-1.2.8}"

HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="$HERE/out"
IMAGE=revoice-shairport-ap2

echo "Building shairport-sync $SPS_REF (AirPlay 2) and nqptp $NQPTP_REF"
echo "for armv7a/Android API 22..."
docker build -f "$HERE/Dockerfile.ap2" -t "$IMAGE" "$HERE"

mkdir -p "$OUT"
docker run --rm \
    -v "$OUT:/out" \
    -v "$HERE/compat:/compat:ro" \
    -v "$HERE/ap2:/ap2:ro" \
    -e SPS_REF="$SPS_REF" \
    -e NQPTP_REF="$NQPTP_REF" \
    "$IMAGE" bash /ap2/in-container.sh

cat <<EOF

Built:
  $OUT/shairport-sync-ap2
  $OUT/nqptp

AirPlay 2 needs BOTH. nqptp is a second process: it must be running, it needs
UDP 319 and 320 to itself, and it needs to be able to bind them — which on a
rooted Echo it can. shairport-sync reads the clock nqptp publishes through a
shared-memory object; on bionic that is a file under /dev (a tmpfs), not POSIX
shared memory, which is what compat/android_shm.c exists for.

The device supervision exists — internal/airplay's PlanNqptp starts nqptp
before the receiver, but only when the shairport-sync it finds reports
AirPlay 2 in its own version string. So a classic binary beside an nqptp is
inert rather than wrong.

Install through the dashboard: Updates -> Streaming endpoints, which verifies
the md5 on arrival and renames into place only on a match. shairport-sync-ap2
goes in as the AirPlay kind -- the device runs ONE shairport-sync and the
firmware asks the file which protocol it speaks -- and nqptp has a kind of its
own. Over a cable, if you would rather:

  adb push $OUT/shairport-sync-ap2 /data/local/bin/shairport-sync
  adb push $OUT/nqptp              /data/local/bin/nqptp
  adb shell chmod 755 /data/local/bin/shairport-sync /data/local/bin/nqptp

**On FireOS this is not enough, and the gap is a firewall rather than a
build.** Every AirPlay 2 session binds two extra TCP sockets on ephemeral
ports the kernel picks, and shairport-sync has no setting for either
(rtsp.c: local_event_port = 0, local_buffered_audio_port = 0). FireOS ships
-P INPUT DROP, so the session negotiates and then stalls -- a speaker that
appears, accepts a connection and plays nothing. internal/netfilter has the
three ways out and why none of them was taken.

**On emOS the question does not arise**: there is no default-deny policy, so
nothing has to be told about a port chosen at runtime.
EOF
