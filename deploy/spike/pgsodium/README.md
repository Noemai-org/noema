# pgsodium spike (SEC-12)

Date: 2026-11-01. Owner: SR. **Test infrastructure only. Nothing here is adopted.** Adoption of pgsodium needs the
checks below to pass in CI, then Core Engineer 1's W4 (EXE-08), then founder approval (AGENT-14). Nothing under `services/`
or `contracts/` uses this directory.

It tests checks T1 to T6 of `docs/security/w2-ed25519-extension-evaluation.md` (docs repository, section 7) on the
Postgres major Compose runs today: **17.11 on Alpine**, same digest as `deploy/compose/docker-compose.yml`. The task
text said 16; the Architect confirmed 17.11. The base image is unchanged (Alpine); if pgsodium does not build on it,
the CI log says so and the choice of another base is recorded in the PR.

## Pins (nothing is downloaded unchecked)

| Input | Pin | How it was checked |
|---|---|---|
| Base image | `postgres:17.11-alpine@sha256:b0f9560a...` | Same digest as Compose; `check_pins.py` enforces a digest |
| libsodium 1.0.22 | SHA-256 `adbdd8f16149e81ac6078a03aca6fc03b592b89ef7b5ed83841c086191be3349`, enforced in the Dockerfile | The release's minisign signature (`libsodium-1.0.22.tar.gz.minisig`, key id `1fe8b442180f62e7`) verified on 2026-11-01 against the project's published key `RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3` (libsodium-doc, installation page). The check was a pure-Python Ed25519 verification of the BLAKE2b-prehashed file and of the trusted comment |
| pgsodium v3.1.11 | commit `a8b8a2739bab1f7a07445b1c911e2bd1abf67784`, fetched by id and compared after checkout | The commit id is the pin; no tarball is used |
| GitHub Actions | full commit ids | `check_pins.py` |

Limit: the libsodium hash is pinned after a signature check done by hand, not inside the build. A future release
bump repeats that check. The commit id of pgsodium is a SHA-1 object id.

## Files

- `Dockerfile`: builds libsodium and pgsodium, copies only the library and the extension files into a fresh Postgres image.
- `gen_vectors.py`: standard-library RFC 8032 implementation. It asserts RFC 8032 section 7.1 TEST 1 and TEST 2 before it writes `tests/vectors.sql`. Run by hand; CI runs the committed output. Keys are labelled SYNTHETIC.
- `tests/00_setup.sql`: the evaluation's trust model: a key table, a `SECURITY DEFINER` verifier with a pinned `search_path`, and an always-enabled trigger, all owned by a migration role; the runtime role `execution_rw` owns nothing. The receipt byte layout here is a spike placeholder; the real canonical bytes are the W1 design (GOV-28).
- `run.sh`: runs the checks and writes a table to the job summary. Authentication (the trigger refuses a forged receipt) and restriction (the role cannot change the control) are tested separately, and one test shows restriction alone refusing a valid receipt, to keep the two apart.

## Known limits of this spike

- T6 shows the insert fails closed and the writer sees the error. It does not test an alert; the `SECURITY_EVENT` and the DEBT-02 channel are the Controller's (W4).
- A superuser or the migration role can still change the control. That is outside what an extension can address (evaluation, section 3).
- `now()` stands in for the event time in key-validity checks. The W1 design decides which clock.
- Performance was not measured here.
