# deploy/compose

The POC topology (PLT-04; PERD §65.1, REQ-OPS-001). Local use only.

| Command | What it does |
|---|---|
| `make up` | creates local secret files if missing, then builds and starts Postgres, Temporal, core, model and the UI, waiting for every health check |
| `make net-matrix` | runs the connection-test matrix against the running stack |
| `make bench-smoke` | zero-case deterministic smoke; refuses to run if `benchmark-heldout` is referenced |
| `make model-bench-up` | the bench profile with `bench-model.yml` added: the model service gets `BENCH_OUT=/bench-out`, a bind mount of the Makefile's `BENCH_OUT` and the invoking user's uid, and writes each call's cost record to `$BENCH_OUT/cost/` (REQ-NFR-007). Only this target adds the file; `make up` and CI never do. Checked with `docker compose config` only (no Docker daemon where it was written) |
| Manual workflow `bench-model-up` | `workflow_dispatch` only, Linux runner: runs `make model-bench-up`, makes the model container write one SYNTHETIC cost file through the service's own telemetry, checks that file on the host (one file, valid JSON, owned by the invoking user), then tears the stack down. It has not been run; until it has, the bind mount and the uid mapping of `bench-model.yml` are untested end to end |
| `make down` | stops the stack, keeps the data volume |
| `make reset` | destroys containers, networks and the data volume of project `noema-local`; refuses any other project name; prints what it will destroy |

## Networks

`edge_net` (ui, core, model; PERD §65.1), `svc_net` (core, model), `data_net` (core, postgres, temporal), `tushe_net` (core), `provider_egress` (model only), `bench_net` (nobody yet; bench services only, unreachable from core and model). All of these except `provider_egress` are Docker-internal. One further network, `ui_publish_net`, has the UI as its only member: it is how the UI publishes its port to localhost, and it is the UI's only route out. It is not one of the six networks of PERD §65.1; it exists so that `edge_net` can be internal (see "Routes out").

Profiles: `dev`, `ci`, `bench`, `demo` all start the same five services for now. The hostility proxy and runner (bench; ENV-09) and the reference partner listener (demo) join later. `netcheck` holds the matrix fixtures and is never started by `make up`.

## Database and secrets

Postgres creates `noema` (owner `noema_core`, a role that is not a superuser and cannot create roles or databases) and, separately, `temporal` and `temporal_visibility` (owner `temporal`, its own password). Noema code never uses the Temporal role. Passwords are random files in `deploy/compose/secrets/` (git-ignored), mounted at `/run/secrets/`. The model service receives no secret and no DB or Temporal variable. The webhook relay is not part of Compose (PERD §65.1).

## The matrix (`netcheck/matrix.py`)

1. The Compose configuration gives each service exactly the networks of PERD §65.1 (written out in the script, independent of the Compose file).
2. A probe that carries each service's networks tries every target. A target must be reachable exactly when a network is shared.
3. The forbidden paths are repeated from inside the real model container.
4. Database roles: `noema_core` is a plain role; neither the core's role nor Temporal's role can connect to the other's databases.
5. Egress: the core and the model without `provider_egress` cannot reach an outside address; the UI still serves on its published port.

## Routes out

The only networks that are not Docker-internal are `provider_egress` (the model, for providers, PERD §65.1) and `ui_publish_net` (the UI, to publish its port). So:

- the **core** has no route out at all;
- the **model** reaches outside only through `provider_egress`;
- the **UI** reaches outside only through `ui_publish_net`;
- only the UI publishes a port, on `127.0.0.1:8082`.

Step 5 of the matrix proves it: a listener on the host stands for an address outside the stack. The core, and a probe carrying the model's networks minus `provider_egress`, must not reach it; the model (through `provider_egress`) and the UI may. Shared environments still need the host firewall rules REQ-OPS-001 also requires.
