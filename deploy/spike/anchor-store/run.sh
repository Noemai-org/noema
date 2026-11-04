#!/usr/bin/env bash
# SEC-14 spike runner (test infrastructure only). Usage: run.sh seaweedfs. Needs docker and python3.
# Builds the store image from its pinned Dockerfile, runs it with ONLY the S3 port published to localhost, and runs checks A1 to A7.
set -eu
STORE="${1:?store name}"
HERE="$(cd "$(dirname "$0")" && pwd)"
C="anchor-spike-$STORE-$$"
cleanup() { docker rm -f "$C" >/dev/null 2>&1 || true; rm -rf "${WORK:-/nonexistent}"; }
trap cleanup EXIT
# Throwaway credentials for this run only: random, never committed, never printed (the scanner rightly refuses key-shaped strings in the repository).
gen() { openssl rand -hex "$1"; }
export SPIKE_ADMIN_AK="ADM$(gen 8)" SPIKE_ADMIN_SK="$(gen 20)" SPIKE_CORE_AK="COR$(gen 8)" SPIKE_CORE_SK="$(gen 20)" SPIKE_VERIFIER_AK="VER$(gen 8)" SPIKE_VERIFIER_SK="$(gen 20)"
WORK="$(mktemp -d)"
docker build -q -t "anchor-spike-$STORE:ci" "$HERE/$STORE" >/dev/null
case "$STORE" in
  seaweedfs)
    sed -e "s/@ADMIN_AK@/$SPIKE_ADMIN_AK/" -e "s/@ADMIN_SK@/$SPIKE_ADMIN_SK/" -e "s/@CORE_AK@/$SPIKE_CORE_AK/" -e "s/@CORE_SK@/$SPIKE_CORE_SK/" \
        -e "s/@VERIFIER_AK@/$SPIKE_VERIFIER_AK/" -e "s/@VERIFIER_SK@/$SPIKE_VERIFIER_SK/" "$HERE/seaweedfs/s3.json.tmpl" > "$WORK/s3.json"
    chmod 0644 "$WORK/s3.json"
    docker run -d --name "$C" -p 127.0.0.1:8333:8333 -v "$WORK/s3.json:/cfg/s3.json:ro" "anchor-spike-$STORE:ci" \
      server -dir=/data -s3 -s3.config=/cfg/s3.json -master.volumeSizeLimitMB=64 -volume.max=3 >/dev/null ;;
  silo)
    docker run -d --name "$C" -p 127.0.0.1:9000:9000 -e "MINIO_ROOT_USER=$SPIKE_ADMIN_AK" -e "MINIO_ROOT_PASSWORD=$SPIKE_ADMIN_SK" \
      "anchor-spike-$STORE:ci" server /data >/dev/null
    for _ in $(seq 1 60); do
      docker exec "$C" mcli alias set l http://127.0.0.1:9000 "$SPIKE_ADMIN_AK" "$SPIKE_ADMIN_SK" >/dev/null 2>&1 && break
      sleep 2
    done
    # A write-only identity (PutObject only) and a read-only verifier identity. The administrator is the root user.
    docker exec -i "$C" sh -c 'cat > /tmp/core.json' <<'JSON'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::anchors/*"]}]}
JSON
    docker exec -i "$C" sh -c 'cat > /tmp/verifier.json' <<'JSON'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:GetObjectVersion","s3:ListBucket","s3:ListBucketVersions"],"Resource":["arn:aws:s3:::anchors","arn:aws:s3:::anchors/*"]}]}
JSON
    docker exec "$C" mcli admin user add l "$SPIKE_CORE_AK" "$SPIKE_CORE_SK"
    docker exec "$C" mcli admin user add l "$SPIKE_VERIFIER_AK" "$SPIKE_VERIFIER_SK"
    docker exec "$C" mcli admin policy create l corewrite /tmp/core.json
    docker exec "$C" mcli admin policy create l verifierread /tmp/verifier.json
    docker exec "$C" mcli admin policy attach l corewrite --user "$SPIKE_CORE_AK"
    docker exec "$C" mcli admin policy attach l verifierread --user "$SPIKE_VERIFIER_AK" ;;
  *) echo "unknown store: $STORE" >&2; exit 2 ;;
esac
PORT=8333; [ "$STORE" = silo ] && PORT=9000
python3 -I "$HERE/run_a.py" "$STORE" "$C" "$PORT" "$(grep -m1 '^FROM' "$HERE/$STORE/Dockerfile")" || { rc=$?; docker logs "$C" 2>&1 | tail -30; exit $rc; }
