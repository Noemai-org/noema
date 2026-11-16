# deploy/compose

The POC topology (PLT-04; PERD §65.1, REQ-OPS-001). Local use only.

| Command | What it does |
|---|---|
| `make up` | creates local secret files if missing, then builds and starts Postgres, Temporal, core, model and the UI, waiting for every health check |
| `make net-matrix` | runs the connection-test matrix against the running stack |
| `make bench-smoke` | zero-case deterministic smoke; refuses to run if `benchmark-heldout` is referenced |
| `make model-bench-up` | the bench profile with `bench-model.yml` added: the model service is started through `model_cost.model_app:app` from `benchmark/model_cost` (mounted read-only; the service's own code reads no benchmark variable, ruling A55), with `BENCH_OUT=/bench-out`, a bind mount of the Makefile's `BENCH_OUT` and the invoking user's uid, and each call's cost record is written to `$BENCH_OUT/cost/` (REQ-NFR-007). Only this target adds the file; `make up` and CI never do. Checked with `docker compose config` only (no Docker daemon where it was written) |
| `make bench-up` / `make bench-down` | the full bench stack for a G1 run: the bench profile with `bench-model.yml` (the model's cost files) and `bench-stack.yml` (the core's stand-in settings, the hostility proxy on the stand-in route table, the runner's output directory). The runner is the `bench-runner` container (`services/bench-runner.yml`) on `bench_net` and `edge_net`; run the suite in it with `docker compose ... exec bench-runner python -m benchmark.runner ...` (the dispatch workflow `bench-g1-stack` does, and captures mock-case's live log). `profiles.py` resolves the bench profile with both overlays |
| `make down` | stops the stack, keeps the data volume |
| `make reset` | destroys containers, networks and the data volume of project `noema-local`; refuses any other project name; prints what it will destroy |

## Networks

`edge_net` (ui, core, model; PERD §65.1), `svc_net` (core, model), `data_net` (core, postgres, temporal), `tushe_net` (core), `provider_egress` (model only), `bench_net` (the oracle side: mock-case, its database, the hostility proxy, the stand-in, their anchors, and the bench runner; unreachable from core and model). All of these except `provider_egress` are Docker-internal. One further network, `ui_publish_net`, has the UI as its only member: it is how the UI publishes its port to localhost, and it is the UI's only route out. It is not one of the six networks of PERD §65.1; it exists so that `edge_net` can be internal (see "Routes out").

Profiles: `dev`, `ci`, `bench`, `demo` all start the same five services for now. The bench runner is in `bench` only and sits on `bench_net` and `edge_net`: it is the one caller that reaches both the oracle side and the core's public API, and `netcheck/profiles.py` keeps the core and the model off `bench_net`. The hostility proxy and runner (bench; ENV-09) and the reference partner listener (demo) join later. `netcheck` holds the matrix fixtures and is never started by `make up`.

## Database and secrets

Postgres creates `noema` (owner `noema_migrator`, a `NOLOGIN` role whose only login is `noema_migration_login`; W1 D14a). The core's login `noema_core` is not a superuser, cannot create roles or databases, owns nothing and has no `CREATE` on the database or on `public`; it is never a member of `noema_migrator`. The migration login's password (`noema_migrator_password`) is mounted into `postgres` (the bootstrap) and nowhere else; the one-shot migrate step (EXE-08) will be the second holder. `postgres/assert_roles.sql` pins all of this, run by `make net-matrix` against the stack and by `postgres/test_init.sh` against a throwaway local cluster. Separately, it creates `temporal` and `temporal_visibility` (owner `temporal`, its own password). Noema code never uses the Temporal role. Passwords are random files in `deploy/compose/secrets/` (git-ignored), mounted at `/run/secrets/`. The model service receives no secret and no DB or Temporal variable. The webhook relay is not part of Compose (PERD §65.1).

## The matrix (`netcheck/matrix.py`)

1. The Compose configuration gives each service exactly the networks of PERD §65.1 (written out in the script, independent of the Compose file).
2. A probe that carries each service's networks tries every target. A target must be reachable exactly when a network is shared.
3. The forbidden paths are repeated from inside the real model container.
4. Database roles: `init.sh` creates every role (a migration never does): the group roles `trust_rw`, `execution_rw`, `outcome_rw`, `evidence_rw`, `orchestrator_rw`, `record_append`, `record_read` and `record_verify` (NOLOGIN), the verifier's login `noema_verify`, the evidence recompute's read-only login `evidence_verify` (a member of no role; migration 0020 grants it SELECT on `context_snapshot` and `evidence_item`), and, for each tenant in `NOEMA_TENANTS` (for example `NOEMA_TENANTS="ten_aa ten_bb"`), one login per power named `<group>__<tenant>`. After the migrations, `postgres/provision-tenants.sh` (run as `noema_core`) adds the `tenant_login` mapping rows; it is idempotent. The passwords are file-mounted secrets (`noema_verify_password`, `evidence_verify_password`, `noema_tenant_login_password`; one shared tenant-login password in the POC for the power logins, DEBT-13; **each emitter login has its own secret file `secrets/emitter_logins/<tenant>__<emitter>`** (ruling A4), which `init.sh` refuses to start without and refuses if it equals any other secret, and which the shared password never opens, because the core process holds every login and the boundary is the database's mapping). Also: `noema_core` is a plain role; neither the core's role nor Temporal's role can connect to the other's databases.
5. Egress: the core and the model without `provider_egress` cannot reach an outside address; the UI still serves on its published port.

## Routes out

The only networks that are not Docker-internal are `provider_egress` (the model, for providers, PERD §65.1) and `ui_publish_net` (the UI, to publish its port). So:

- the **core** has no route out at all;
- the **model** reaches outside only through `provider_egress`;
- the **UI** reaches outside only through `ui_publish_net`;
- only the UI publishes a port, on `127.0.0.1:8082`.

Step 5 of the matrix proves it: a listener on the host stands for an address outside the stack. The core, and a probe carrying the model's networks minus `provider_egress`, must not reach it; the model (through `provider_egress`) and the UI may. Shared environments still need the host firewall rules REQ-OPS-001 also requires.

## mTLS between the core and the model service (noema#555)

The model service's listener requires a client certificate signed by the stack's local CA (uvicorn's own `--ssl-*` options, set as `UVICORN_SSL_*` in `services/model/Dockerfile`, `CERT_REQUIRED`); there is no plain HTTP port. `secrets_local.sh` calls `tls_local.sh`, which makes a throwaway CA and three certificates into the git-ignored `secrets/tls/` (OD-P-22) and deletes the CA key: `model.crt/.key` (the model's server certificate), `core-client.crt/.key` (the core's identity) and `model-health.crt/.key` (the model's own health-check client). Each service mounts only its own key and the CA certificate; `netcheck/profiles.py` fails if the core mounts the model's key or the model the core's. The model's health check presents `model-health` on the same listener. The core is given the setting names `NOEMA_MODEL_URL` (`https://model:8081`), `NOEMA_MODEL_TLS_CA_FILE`, `NOEMA_MODEL_TLS_CERT_FILE`, `NOEMA_MODEL_TLS_KEY_FILE`; its client is the Engineering Lead's. SYNTHETIC local material; real certificates are the OQ-09 decision. The compile route stays unwired in `main.py` until this is merged.

