#!/usr/bin/env bash
# SEC-12 spike runner (test infrastructure only). Usage: run.sh <image> . Needs docker. Exit 0 only if every check passes.
# Every check names the evaluation's test id (T1 to T6, docs/security/w2-ed25519-extension-evaluation.md section 7).
set -u
IMAGE="${1:?image}"
HERE="$(cd "$(dirname "$0")" && pwd)"
C="pgsodium-spike-$$"
PASS=0; FAIL=0; ROWS=()
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}"

cleanup() { docker rm -f "$C" >/dev/null 2>&1 || true; }
trap cleanup EXIT

record() { # id label ok detail
  if [ "$3" = 1 ]; then PASS=$((PASS+1)); ROWS+=("| $1 | $2 | PASS | |"); echo "PASS $1 $2"
  else FAIL=$((FAIL+1)); ROWS+=("| $1 | $2 | **FAIL** | $(printf '%s' "$4" | tr '\n|' '  ' | cut -c1-300) |"); echo "FAIL $1 $2: $4"; fi
}
pg()  { docker exec -i "$C" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d spike "$@" 2>&1; }
ex()  { docker exec -i "$C" psql -X -q -At -v ON_ERROR_STOP=1 -U execution_rw -d spike "$@" 2>&1; }
expect_eq() { # id label expected actual
  if [ "$3" = "$4" ]; then record "$1" "$2" 1; else record "$1" "$2" 0 "expected [$3] got [$4]"; fi
}
expect_ok() { # id label user sql
  local out rc; out="$($3 -c "$4")"; rc=$?
  if [ $rc -eq 0 ]; then record "$1" "$2" 1; else record "$1" "$2" 0 "unexpected error: $out"; fi
}
expect_fail() { # id label user sql substring
  local out rc; out="$($3 -c "$4")"; rc=$?
  if [ $rc -ne 0 ] && printf '%s' "$out" | grep -qF -- "$5"; then record "$1" "$2" 1
  else record "$1" "$2" 0 "rc=$rc, wanted error containing [$5], got [$out]"; fi
}
ins() { echo "INSERT INTO spike.execution_receipt SELECT tenant_id, grant_id, attempt, state, key_id, signature FROM public.spike_receipt_sig WHERE name = '$1'"; }

docker run -d --name "$C" -e POSTGRES_HOST_AUTH_METHOD=trust -v "$HERE/tests:/tests:ro" "$IMAGE" >/dev/null
for _ in $(seq 1 60); do docker exec "$C" pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done
sleep 2; docker exec "$C" pg_isready -U postgres >/dev/null 2>&1 || { echo "database did not start"; docker logs "$C"; exit 1; }
docker exec "$C" psql -X -q -U postgres -c 'CREATE DATABASE spike' >/dev/null

# T1: the extension loads on the chosen major, without shared_preload_libraries.
out="$(docker exec -i "$C" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d spike -f /tests/00_setup.sql 2>&1)"; rc=$?
record T1 "setup and CREATE EXTENSION pgsodium succeed" $([ $rc -eq 0 ] && echo 1 || echo 0) "$out"
if [ $rc -ne 0 ]; then echo "$out"; fi
expect_eq T1 "PostgreSQL major is 17" 17 "$(pg -c 'SHOW server_version_num' | cut -c1-2)"
expect_eq T1 "shared_preload_libraries is empty" "" "$(pg -c 'SHOW shared_preload_libraries')"
expect_eq T1 "pgsodium version is 3.1.11" 3.1.11 "$(pg -c "SELECT extversion FROM pg_extension WHERE extname='pgsodium'")"
echo "evidence: $(docker exec "$C" sha256sum /usr/local/lib/postgresql/pgsodium.so | cut -c1-64) pgsodium.so; $(docker exec "$C" sh -c 'ls /usr/local/lib/libsodium.so.*' | head -1)"

# T2 and T3: known answers, then malleable and non-canonical encodings. Direct call and through the wrapper.
for mode in direct wrapper; do
  if [ $mode = direct ]; then f='pgsodium.crypto_sign_verify_detached(sig, msg, pk)'; else f='spike.verify_ed25519(sig, msg, pk)'; fi
  bad="$(pg -c "SELECT coalesce(string_agg(name, ','), '') FROM spike_vec WHERE (name LIKE 'rfc%' OR name LIKE 'flip%' OR name = 'wrong_key') AND ($f) IS DISTINCT FROM expect")"
  expect_eq T2 "known answers and altered inputs ($mode)" "" "$bad"
  bad="$(pg -c "SELECT coalesce(string_agg(name, ','), '') FROM spike_vec WHERE NOT (name LIKE 'rfc%' OR name LIKE 'flip%' OR name = 'wrong_key') AND ($f) IS DISTINCT FROM expect")"
  expect_eq T3 "malleable, small-order and non-canonical vectors all refused ($mode)" "" "$bad"
done
expect_eq T3 "vector count is the generated count" 14 "$(pg -c 'SELECT count(*) FROM spike_vec')"

# T4: the wrapper and the trigger, as the runtime role.
expect_fail T4 "NULL signature refused" ex "SELECT spike.verify_ed25519(NULL, '\\x00', '\\x00')" "NULL argument"
expect_fail T4 "63-byte signature refused" ex "SELECT spike.verify_ed25519(decode(repeat('00',63),'hex'), '\\x00', decode(repeat('00',32),'hex'))" "must be 64 bytes"
expect_fail T4 "65-byte signature refused" ex "SELECT spike.verify_ed25519(decode(repeat('00',65),'hex'), '\\x00', decode(repeat('00',32),'hex'))" "must be 64 bytes"
expect_fail T4 "empty signature refused" ex "SELECT spike.verify_ed25519(''::bytea, '\\x00', decode(repeat('00',32),'hex'))" "must be 64 bytes"
expect_fail T4 "31-byte public key refused" ex "SELECT spike.verify_ed25519(decode(repeat('00',64),'hex'), '\\x00', decode(repeat('00',31),'hex'))" "must be 32 bytes"
expect_ok   T4 "valid receipt accepted" ex "$(ins ok)"
expect_ok   T4 "second valid receipt accepted" ex "$(ins ok_second)"
for n in replay_grant replay_tenant replay_attempt replay_state signed_by_other_key forged_well_formed forged_shape_only; do
  expect_fail T4 "$n refused" ex "$(ins $n)" "does not verify"
done
for n in revoked_key not_yet_valid_key expired_key unknown_key; do
  expect_fail T4 "$n refused" ex "$(ins $n)" "not an active trusted key"
done
expect_fail T4 "63-byte receipt signature refused" ex "INSERT INTO spike.execution_receipt SELECT tenant_id, 'g20', attempt, state, key_id, substr(signature,1,63) FROM public.spike_receipt_sig WHERE name='ok'" "must be 64 bytes"
expect_fail T4 "65-byte receipt signature refused" ex "INSERT INTO spike.execution_receipt SELECT tenant_id, 'g21', attempt, state, key_id, signature || '\\x00'::bytea FROM public.spike_receipt_sig WHERE name='ok'" "must be 64 bytes"
expect_fail T4 "empty receipt signature refused" ex "INSERT INTO spike.execution_receipt SELECT tenant_id, 'g22', attempt, state, key_id, ''::bytea FROM public.spike_receipt_sig WHERE name='ok'" "must be 64 bytes"
expect_fail T4 "NULL receipt signature refused" ex "INSERT INTO spike.execution_receipt SELECT tenant_id, 'g23', attempt, state, key_id, NULL FROM public.spike_receipt_sig WHERE name='ok'" "null value"
expect_eq   T4 "only the two valid receipts are in the table" 2 "$(pg -c 'SELECT count(*) FROM spike.execution_receipt')"

# T5: authentication (the trigger refuses a forged receipt) and restriction (the role cannot change the control) are separate tests.
expect_fail T5 "AUTHENTICATION: execution_rw INSERT of a well-formed forged receipt (the DEBT-01 case) is refused" ex "$(ins forged_shape_only)" "does not verify"
expect_fail T5 "calling the trigger function directly is refused" ex "SELECT spike.verify_receipt()" "trigger"
expect_fail T5 "pg_temp shadowing of octet_length does not weaken the verifier" ex "CREATE FUNCTION pg_temp.octet_length(bytea) RETURNS int LANGUAGE sql AS 'SELECT 64'; $(ins forged_shape_only)" "does not verify"
expect_fail T5 "RESTRICTION: INSERT into trusted_key" ex "INSERT INTO spike.trusted_key (key_id, public_key, valid_from) VALUES ('kEvil', decode(repeat('00',32),'hex'), now())" "permission denied"
expect_fail T5 "RESTRICTION: UPDATE trusted_key" ex "UPDATE spike.trusted_key SET public_key = decode(repeat('01',32),'hex')" "permission denied"
expect_fail T5 "RESTRICTION: DELETE trusted_key" ex "DELETE FROM spike.trusted_key" "permission denied"
expect_fail T5 "RESTRICTION: TRUNCATE trusted_key" ex "TRUNCATE spike.trusted_key" "permission denied"
expect_fail T5 "RESTRICTION: DISABLE TRIGGER" ex "ALTER TABLE spike.execution_receipt DISABLE TRIGGER verify_receipt" "must be owner"
expect_fail T5 "RESTRICTION: DROP TRIGGER" ex "DROP TRIGGER verify_receipt ON spike.execution_receipt" "must be owner"
expect_fail T5 "RESTRICTION: DROP FUNCTION verify_receipt" ex "DROP FUNCTION spike.verify_receipt()" "must be owner"
expect_fail T5 "RESTRICTION: replace the verifier" ex "CREATE OR REPLACE FUNCTION spike.verify_ed25519(bytea,bytea,bytea) RETURNS boolean LANGUAGE sql AS 'SELECT true'" "permission denied"
expect_fail T5 "RESTRICTION: alter the verifier's search_path" ex "ALTER FUNCTION spike.verify_receipt() SET search_path = public" "must be owner"
expect_fail T5 "RESTRICTION: SET ROLE to the migration role" ex "SET ROLE spike_migration" "permission denied"
expect_fail T5 "RESTRICTION: SET session_replication_role" ex "SET session_replication_role = replica" "permission denied"
expect_fail T5 "RESTRICTION: grant itself rights on trusted_key" ex "GRANT ALL ON spike.trusted_key TO execution_rw" "permission denied"
expect_ok   T5 "restriction control only: REVOKE INSERT is applied by the owner" pg "REVOKE INSERT ON spike.execution_receipt FROM execution_rw"
expect_fail T5 "RESTRICTION ALONE refuses even a valid receipt (it is not authentication)" ex "$(ins ok_third)" "permission denied"
expect_ok   T5 "INSERT grant restored for the T6 tests" pg "GRANT INSERT ON spike.execution_receipt TO execution_rw"
expect_fail T5 "a superuser in session_replication_role=replica still hits the always-enabled trigger" pg "SET session_replication_role = replica; $(ins forged_well_formed)" "does not verify"

# T6: extension unavailable. The insert must fail closed with the error visible to the writer, and add no row.
docker exec "$C" mv /usr/local/lib/postgresql/pgsodium.so /usr/local/lib/postgresql/pgsodium.so.away
expect_fail T6 "library file missing: a VALID receipt is refused, not accepted unverified" ex "$(ins ok_third)" "pgsodium"
docker exec "$C" mv /usr/local/lib/postgresql/pgsodium.so.away /usr/local/lib/postgresql/pgsodium.so
expect_eq   T6 "no row was added while the library was missing" 2 "$(pg -c 'SELECT count(*) FROM spike.execution_receipt')"
expect_ok   T6 "the extension is dropped" pg "DROP EXTENSION pgsodium"
expect_fail T6 "extension dropped: a VALID receipt is refused" ex "$(ins ok_third)" "pgsodium"
expect_eq   T6 "no row was added while the extension was absent" 2 "$(pg -c 'SELECT count(*) FROM spike.execution_receipt')"

{
  echo "## pgsodium spike (SEC-12): PostgreSQL 17.11 on Alpine"
  echo "Test infrastructure only; nothing is adopted. SYNTHETIC keys and data."
  echo; echo "| Test | Check | Result | Detail |"; echo "|---|---|---|---|"
  printf '%s\n' "${ROWS[@]}"
  echo; echo "**$PASS passed, $FAIL failed.**"
  echo "T6 shows the insert fails closed. It does not show an alert: the SECURITY_EVENT and DEBT-02 channel belong to the Controller (W4)."
} | tee -a "$SUMMARY"
[ "$FAIL" -eq 0 ]
