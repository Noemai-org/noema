#!/usr/bin/env python3
"""Connection-test matrix (PLT-04; REQ-OPS-001, PERD §65.1). Standard library only.

Needs the stack running (`make up`). It starts the netcheck sentinels, then:
  1. checks that the Compose configuration gives each service exactly the networks of PERD §65.1;
  2. probes every target from a probe that carries each service's networks and requires that a
     target is reachable exactly when a network is shared;
  3. repeats the forbidden paths from inside the real model container;
  4. checks the database roles and the model service's environment.
It prints the matrix and exits 1 on the first mismatch of any kind.
"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
COMPOSE = ["docker", "compose", "-f", str(HERE.parent / "docker-compose.yml"), "--profile", "dev", "--profile", "netcheck"]

# PERD §65.1, written out independently of the Compose file so that drift is caught.
NETWORKS = {
    "ui": {"edge_net"},
    "core": {"edge_net", "svc_net", "data_net", "tushe_net"},
    "model": {"edge_net", "svc_net", "provider_egress"},
    "postgres": {"data_net"},
    "temporal": {"data_net"},
    "sentinel-tushe": {"tushe_net"},
    "sentinel-provider": {"provider_egress"},
    "sentinel-bench": {"bench_net"},
}
TARGETS = {  # service -> host:port a probe connects to
    "postgres": "postgres:5432", "temporal": "temporal:7233", "core": "core:8080", "model": "model:8081",
    "ui": "ui:8082", "sentinel-tushe": "sentinel-tushe:9000", "sentinel-provider": "sentinel-provider:9000",
    "sentinel-bench": "sentinel-bench:9000",
}
# Paths the brief names as forbidden. Each must come out refused.
MUST_BE_REFUSED = [
    ("model", "postgres"), ("model", "temporal"), ("model", "sentinel-tushe"), ("model", "sentinel-bench"),
    ("core", "sentinel-provider"), ("core", "sentinel-bench"), ("ui", "postgres"), ("ui", "temporal"),
    ("ui", "sentinel-tushe"), ("ui", "sentinel-provider"), ("ui", "sentinel-bench"),
]
failures: list[str] = []


def run(cmd: list[str], stdin: str | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, input=stdin, capture_output=True, text=True)


def fail(message: str) -> None:
    failures.append(message)
    print("  FAIL " + message)


def check_membership() -> None:
    print("1. network membership from `docker compose config`")
    config = json.loads(run(COMPOSE + ["config", "--format", "json"]).stdout)
    for name, expected in NETWORKS.items():
        actual = set(config["services"][name].get("networks") or {})
        status = "ok" if actual == expected else "MISMATCH"
        print(f"  {name:18} {sorted(actual)} {status}")
        if actual != expected:
            fail(f"{name} is on {sorted(actual)}, PERD §65.1 says {sorted(expected)}")
    for probe, service in (("probe-core", "core"), ("probe-model", "model"), ("probe-ui", "ui")):
        if set(config["services"][probe]["networks"]) != NETWORKS[service]:
            fail(f"{probe} does not carry the networks of {service}")
    for net, spec in config["networks"].items():
        if net == "bench_net" and any("bench_net" in (config["services"][s].get("networks") or {}) for s in ("core", "model")):
            fail("bench_net is reachable from core or model")
    for net in ("svc_net", "data_net", "tushe_net", "bench_net"):
        if not config["networks"][net].get("internal"):
            fail(f"{net} must be internal")


def check_matrix() -> dict:
    print("2. reachability, probe -> target ('open' only where a network is shared)")
    targets = list(TARGETS.values())
    observed: dict[str, dict[str, str]] = {}
    for service in ("core", "model", "ui"):
        out = run(COMPOSE + ["run", "--rm", "--no-deps", "-T", f"probe-{service}", "python", "/probe.py"] + targets)
        try:
            observed[service] = json.loads(out.stdout.strip().splitlines()[-1])
        except (ValueError, IndexError):
            fail(f"probe-{service} gave no result: {out.stderr.strip()[:200]}")
            observed[service] = {}
    header = "  {:8}".format("from\\to") + "".join(f"{t:>18}" for t in TARGETS)
    print(header)
    for service in ("core", "model", "ui"):
        cells = []
        for target, hostport in TARGETS.items():
            if target == service:
                cells.append("-")
                continue
            want = "open" if NETWORKS[service] & NETWORKS[target] else "refused"
            got = observed[service].get(hostport, "none")
            cells.append(got if got == want else f"{got}!={want}")
            if got != want:
                fail(f"{service} -> {target}: expected {want}, saw {got}")
        print("  {:8}".format(service) + "".join(f"{c:>18}" for c in cells))
    for source, target in MUST_BE_REFUSED:
        if observed.get(source, {}).get(TARGETS[target]) != "refused":
            fail(f"forbidden path {source} -> {target} was not refused")
    return observed


def check_real_model() -> None:
    print("3. forbidden paths from inside the real model container")
    probe = (HERE / "probe.py").read_text()
    out = run(COMPOSE + ["exec", "-T", "model", "python", "-"] + [TARGETS[t] for t in ("postgres", "temporal", "sentinel-tushe", "sentinel-bench", "core")], stdin=probe)
    try:
        seen = json.loads(out.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        fail(f"exec in model gave no result: {out.stderr.strip()[:200]}")
        return
    for target in ("postgres", "temporal", "sentinel-tushe", "sentinel-bench"):
        print(f"  model -> {target:16} {seen[TARGETS[target]]}")
        if seen[TARGETS[target]] != "refused":
            fail(f"real model container reached {target}")
    print(f"  model -> core             {seen[TARGETS['core']]} (allowed: svc_net)")
    if seen[TARGETS["core"]] != "open":
        fail("real model container cannot reach core on svc_net")
    env = run(COMPOSE + ["exec", "-T", "model", "env"]).stdout
    bad = [line.split("=")[0] for line in env.splitlines() if line.split("=")[0].startswith(("NOEMA_DB", "NOEMA_TEMPORAL", "POSTGRES", "TEMPORAL"))]
    print(f"  model environment names with DB or Temporal settings: {bad or 'none'}")
    if bad:
        fail(f"model environment holds {bad}")
    secrets = run(COMPOSE + ["exec", "-T", "model", "sh", "-c", "ls /run/secrets 2>/dev/null | wc -l"]).stdout.strip()
    print(f"  model secret files mounted: {secrets}")
    if secrets != "0":
        fail("model has secrets mounted")


def psql(user_secret: str, user: str, db: str) -> subprocess.CompletedProcess:
    script = f'PGPASSWORD="$(cat /run/secrets/{user_secret})" psql -h 127.0.0.1 -U {user} -d {db} -tAc "select 1"'
    return run(COMPOSE + ["exec", "-T", "postgres", "sh", "-c", script])


def check_database() -> None:
    print("4. database roles")
    role = run(COMPOSE + ["exec", "-T", "postgres", "psql", "-U", "postgres", "-tAc",
                          "select rolsuper, rolcreatedb, rolcreaterole from pg_roles where rolname = 'noema_core'"]).stdout.strip()
    print(f"  noema_core (super, createdb, createrole): {role}")
    if role != "f|f|f":
        fail(f"noema_core is not a plain role: {role}")
    for user, secret, db, allowed in (("noema_core", "noema_core_password", "noema", True), ("noema_core", "noema_core_password", "temporal", False),
                                     ("temporal", "temporal_password", "temporal", True), ("temporal", "temporal_password", "noema", False)):
        ok = psql(secret, user, db).returncode == 0
        print(f"  {user} -> database {db:9} {'connects' if ok else 'refused'}")
        if ok != allowed:
            fail(f"{user} -> {db}: expected {'connect' if allowed else 'refusal'}")


def main() -> int:
    up = run(COMPOSE + ["up", "-d", "--wait", "sentinel-tushe", "sentinel-provider", "sentinel-bench"])
    if up.returncode != 0:
        print(up.stderr)
        return 2
    check_membership()
    check_matrix()
    check_real_model()
    check_database()
    run(COMPOSE + ["rm", "-sf", "sentinel-tushe", "sentinel-provider", "sentinel-bench"])
    if failures:
        print(f"\nmatrix FAIL: {len(failures)} mismatches")
        return 1
    print(f"\nmatrix OK: {len(MUST_BE_REFUSED)} forbidden paths refused, every other path as PERD §65.1 allows")
    return 0


if __name__ == "__main__":
    sys.exit(main())
