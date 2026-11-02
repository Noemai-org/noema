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
  silo)
    docker run -d --name "$C" -p 127.0.0.1:9000:9000 -e MINIO_ROOT_USER=SYNTHETICADMINKEY0001 -e MINIO_ROOT_PASSWORD=synthetic-admin-secret-0001-not-a-real-secret \
      "anchor-spike-$STORE:ci" server /data >/dev/null
    for _ in $(seq 1 60); do
      docker exec "$C" mcli alias set l http://127.0.0.1:9000 SYNTHETICADMINKEY0001 synthetic-admin-secret-0001-not-a-real-secret >/dev/null 2>&1 && break
      sleep 2
    done
    # A write-only identity (PutObject only) and a read-only verifier identity. The administrator is the root user.
    docker exec -i "$C" sh -c 'cat > /tmp/core.json' <<'JSON'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::anchors/*"]}]}
JSON
    docker exec -i "$C" sh -c 'cat > /tmp/verifier.json' <<'JSON'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:GetObjectVersion","s3:ListBucket","s3:ListBucketVersions"],"Resource":["arn:aws:s3:::anchors","arn:aws:s3:::anchors/*"]}]}
JSON
    docker exec "$C" mcli admin user add l SYNTHETICCOREKEY00001 synthetic-core-secret-0001-not-a-real-secret
    docker exec "$C" mcli admin user add l SYNTHETICVERIFYKEY001 synthetic-verify-secret-001-not-a-real-secret
    docker exec "$C" mcli admin policy create l corewrite /tmp/core.json
    docker exec "$C" mcli admin policy create l verifierread /tmp/verifier.json
    docker exec "$C" mcli admin policy attach l corewrite --user SYNTHETICCOREKEY00001
    docker exec "$C" mcli admin policy attach l verifierread --user SYNTHETICVERIFYKEY001 ;;
  *) echo "unknown store: $STORE" >&2; exit 2 ;;
esac
PORT=8333; [ "$STORE" = silo ] && PORT=9000
python3 -I "$HERE/run_a.py" "$STORE" "$C" "$PORT" "$(grep -m1 '^FROM' "$HERE/$STORE/Dockerfile")" || { rc=$?; docker logs "$C" 2>&1 | tail -30; exit $rc; }
