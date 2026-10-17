# deploy/compose

The POC topology (PLT-04; PERD §65.1, REQ-OPS-001). Local use only.

| Command | What it does |
|---|---|
| `make up` | creates local secret files if missing, then builds and starts Postgres, Temporal, core, model and the UI, waiting for every health check |
| `make net-matrix` | runs the connection-test matrix against the running stack |
| `make bench-smoke` | zero-case deterministic smoke; refuses to run if `benchmark-heldout` is referenced |
| `make down` | stops the stack, keeps the data volume |
| `make reset` | destroys containers, networks and the data volume of project `noema-local`; refuses any other project name; prints what it will destroy |

## Networks

`edge_net` (ui, core, model), `svc_net` (core, model), `data_net` (core, postgres, temporal), `tushe_net` (core), `provider_egress` (model only), `bench_net` (nobody yet; bench services only, unreachable from core and model). `svc_net`, `data_net`, `tushe_net` and `bench_net` are Docker-internal.

Profiles: `dev`, `ci`, `bench`, `demo` all start the same five services for now. The hostility proxy and runner (bench; ENV-09) and the reference partner listener (demo) join later. `netcheck` holds the matrix fixtures and is never started by `make up`.

## Database and secrets

Postgres creates `noema` (owner `noema_core`, a role that is not a superuser and cannot create roles or databases) and, separately, `temporal` and `temporal_visibility` (owner `temporal`, its own password). Noema code never uses the Temporal role. Passwords are random files in `deploy/compose/secrets/` (git-ignored), mounted at `/run/secrets/`. The model service receives no secret and no DB or Temporal variable. The webhook relay is not part of Compose (PERD §65.1).

## The matrix (`netcheck/matrix.py`)

1. The Compose configuration gives each service exactly the networks of PERD §65.1 (written out in the script, independent of the Compose file).
2. A probe that carries each service's networks tries every target. A target must be reachable exactly when a network is shared.
3. The forbidden paths are repeated from inside the real model container.
4. Database roles: `noema_core` is a plain role; neither the core's role nor Temporal's role can connect to the other's databases.

## Limit to know

`edge_net` is not internal, because the UI publishes a port to localhost through it. Core and model are on it, so on a developer machine they have NAT egress through that network. The PERD's rule that the core has no provider path is enforced here by network membership, and in shared environments by the host firewall rules REQ-OPS-001 also requires.
