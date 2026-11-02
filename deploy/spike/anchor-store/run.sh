#!/usr/bin/env bash
# SEC-14 spike runner (test infrastructure only). Usage: run.sh seaweedfs. Needs docker and python3.
# Builds the store image from its pinned Dockerfile, runs it with ONLY the S3 port published to localhost, and runs checks A1 to A7.
set -eu
STORE="${1:?store name}"
HERE="$(cd "$(dirname "$0")" && pwd)"
C="anchor-spike-$STORE-$$"
cleanup() { docker rm -f "$C" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker build -q -t "anchor-spike-$STORE:ci" "$HERE/$STORE" >/dev/null
case "$STORE" in
  seaweedfs)
    docker run -d --name "$C" -p 127.0.0.1:8333:8333 -v "$HERE/seaweedfs/s3.json:/cfg/s3.json:ro" "anchor-spike-$STORE:ci" \
      server -dir=/data -s3 -s3.config=/cfg/s3.json -master.volumeSizeLimitMB=64 -volume.max=3 >/dev/null ;;
  *) echo "unknown store: $STORE" >&2; exit 2 ;;
esac
python3 -I "$HERE/run_a.py" "$STORE" "$C" 8333 "$(grep -m1 '^FROM' "$HERE/$STORE/Dockerfile")" || { rc=$?; docker logs "$C" 2>&1 | tail -30; exit $rc; }
