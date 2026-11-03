#!/usr/bin/env python3
"""Connection-test matrix (PLT-04; REQ-OPS-001, PERD §65.1). Standard library only.

Needs the stack running (`make up`). It starts the netcheck sentinels, then:
  1. checks that the Compose configuration gives each service exactly the networks of PERD §65.1;
  2. probes every target from a probe that carries each service's networks and requires that a
     target is reachable exactly when a network is shared;
  3. repeats the forbidden paths from inside the real model container;
  4. checks the database roles and the model service's environment;
  5. egress: a listener on the host stands for an address outside the stack. The core, and the model without
     provider_egress, must not reach it; the UI (through ui_publish_net) and the model (through provider_egress)
     may. Only the UI publishes a port, and it still serves it.
It prints the matrix and exits 1 on the first mismatch of any kind.
"""
import json
import socket
import subprocess
import sys
import threading
import urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent
COMPOSE = ["docker", "compose", "-f", str(HERE.parent / "docker-compose.yml"), "--profile", "dev", "--profile", "netcheck"]

# PERD §65.1, written out independently of the Compose file so that drift is caught.
NETWORKS = {
    "ui": {"edge_net", "ui_publish_net"},
    "model-no-egress": {"edge_net", "svc_net"},
    "core": {"edge_net", "svc_net", "data_net", "tushe_net"},
    "model": {"edge_net", "svc_net", "provider_egress"},
    "postgres": {"data_net"},
    "temporal": {"data_net"},
    "sentinel-tushe": {"tushe_net"},
    "sentinel-provider": {"provider_egress"},
    "sentinel-bench": {"bench_net"},
    # mock-case (ENV-08): the case API on tushe_net, the truth and control API on bench_net. "mock-case-truth" is
    # the truth listener of the same container, modelled as a bench_net-only target: its address is the bench_net
    # one, so tushe_net callers must be refused.
    "mock-case": {"tushe_net", "bench_net"},
    "mock-case-truth": {"bench_net"},
    "mock-case-db": {"bench_net"},
    # hostility proxy (ENV-09): the data plane on tushe_net, the control API on bench_net only (a listener of the same
    # container, modelled like mock-case-truth), tushe_egress as its one route out.
    "hostility-proxy": {"tushe_net", "bench_net", "tushe_egress"},
    "hostility-proxy-control": {"bench_net"},
    # webhook relay (ENV-13): on tushe_net, so the core can pull; tushe_egress is only so Docker can publish its port.
    "webhook-relay": {"tushe_net", "tushe_egress"},
}
TARGETS = {  # service -> host:port a probe connects to
    "postgres": "postgres:5432", "temporal": "temporal:7233", "core": "core:8080", "model": "model:8081",
    "ui": "ui:8082", "sentinel-tushe": "sentinel-tushe:9000", "sentinel-provider": "sentinel-provider:9000",
    "sentinel-bench": "sentinel-bench:9000",
    "mock-case": "mock-case:8090", "mock-case-truth": "mock-case:8091", "mock-case-db": "mock-case-db:5432",
    "hostility-proxy": "hostility-proxy:8080", "hostility-proxy-control": "hostility-proxy:8081",
    "webhook-relay": "webhook-relay:8090",
}
# Paths the brief names as forbidden. Each must come out refused.
MUST_BE_REFUSED = [
    ("model", "postgres"), ("model", "temporal"), ("model", "sentinel-tushe"), ("model", "sentinel-bench"),
    ("core", "sentinel-provider"), ("core", "sentinel-bench"), ("ui", "postgres"), ("ui", "temporal"),
    ("ui", "sentinel-tushe"), ("ui", "sentinel-provider"), ("ui", "sentinel-bench"),
    # mock-case (REQ-ENV-002): neither the core nor the model nor the UI reaches the truth endpoint or its database.
    ("core", "mock-case-truth"), ("model", "mock-case-truth"), ("ui", "mock-case-truth"),
    ("core", "mock-case-db"), ("model", "mock-case-db"), ("ui", "mock-case-db"),
    ("model", "mock-case"), ("ui", "mock-case"),
    # hostility proxy (REQ-ENV-005): the control API is for the benchmark runner only; the data plane is for the core.
    ("core", "hostility-proxy-control"), ("model", "hostility-proxy-control"), ("ui", "hostility-proxy-control"),
    ("model", "hostility-proxy"), ("ui", "hostility-proxy"),
    # webhook relay (DEC-2026-10-16 E6): only the core pulls from it; the model and the UI never reach it.
    ("model", "webhook-relay"), ("ui", "webhook-relay"),
]
# Networks that are not Docker-internal, per service: the only routes out of the stack (PERD §65.1).
EGRESS_NETWORKS = {"core": set(), "postgres": set(), "temporal": set(), "model": {"provider_egress"}, "ui": {"ui_publish_net"},
                   "mock-case": set(), "mock-case-db": set(), "hostility-proxy": {"tushe_egress"}, "webhook-relay": {"tushe_egress"}}
# What the egress rows must show for each probe: can it reach an address outside the stack?
OUTSIDE_EXPECTED = {"core": False, "model-no-egress": False, "model": True, "ui": True}
# TEN-01 (internal, task ENV-14): "No runtime container can reach the oracle (network and credential)". In this stack the
# oracle's stand-ins are the mock-case truth endpoint and its database, the bench_net sentinel and the hostility proxy's control
# API; the credentials that open them live in the named volumes below. Tushe's own oracle is Tushe's, and Noema holds no route
# to it (G0-T).
RUNTIME = ("core", "model", "ui")
ORACLE_TARGETS = ("mock-case-truth", "mock-case-db", "sentinel-bench", "hostility-proxy-control")
ORACLE_CREDENTIAL_VOLUMES = {"mock_case_bench_credential", "mock_case_db_credentials", "hostility_proxy_credentials", "mock_case_runtime_credential"}
ORACLE_MARKERS = ("ORACLE", "BENCH", "HELDOUT", "ADMIN", "FIXTURE_CRED")  # in a runtime service's variable names, values, secrets or volumes
RUNTIME_SECRETS = {"core": {"noema_core_password"}, "model": set(), "ui": set()}
failures: list[str] = []


def ten01_findings(observed: dict, config: dict) -> list[str]:
    """Every way a runtime container could reach the oracle: by network (a probe from the runtime reached an oracle target) or
    by credential (an oracle credential in its environment, secrets or volumes). Empty when TEN-01 holds. Pure: tested offline."""
    found = []
    for service in RUNTIME:
        for target in ORACLE_TARGETS:
            seen = observed.get(service, {}).get(TARGETS[target], "none")
            if seen != "refused":
                found.append(f"network: {service} -> {target} was {seen}, not refused")
        spec = config["services"][service]
        env = spec.get("environment") or {}
        items = list(env.items()) if isinstance(env, dict) else [tuple(str(e).split("=", 1) + [""])[:2] for e in env]
        for name, value in items:
            text = f"{name}={value}".upper()
            if any(m in text for m in ORACLE_MARKERS):
                found.append(f"credential: {service} environment {name} names an oracle credential")
        secrets = {x if isinstance(x, str) else x.get("source") for x in spec.get("secrets") or []}
        if secrets - RUNTIME_SECRETS[service]:
            found.append(f"credential: {service} has secrets {sorted(secrets - RUNTIME_SECRETS[service])}")
        for vol in spec.get("volumes") or []:
            source = vol if isinstance(vol, str) else (vol.get("source") or "")
            if any(c in str(source) for c in ORACLE_CREDENTIAL_VOLUMES) or any(m in str(source).upper() for m in ORACLE_MARKERS):
                found.append(f"credential: {service} mounts {source}")
    return found


def check_ten01(observed: dict) -> None:
    print("TEN-01. no runtime container can reach the oracle: network and credential")
    config = json.loads(run(COMPOSE + ["config", "--format", "json"]).stdout)
    findings = ten01_findings(observed, config)
    for f in findings:
        fail("TEN-01 " + f)
    print(f"  {len(RUNTIME) * len(ORACLE_TARGETS)} network paths refused, {len(RUNTIME)} runtime services free of oracle credentials: {'ok' if not findings else 'MISMATCH'}")
    print(f"TEN_RESULT id=TEN-01 seed=none pass={str(not findings).lower()}")


def run(cmd: list[str], stdin: str | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, input=stdin, capture_output=True, text=True)


def fail(message: str) -> None:
    failures.append(message)
    print("  FAIL " + message)


def check_membership() -> None:
    print("1. network membership from `docker compose config`")
    config = json.loads(run(COMPOSE + ["config", "--format", "json"]).stdout)
    for name, expected in NETWORKS.items():
        if name in ("model-no-egress", "mock-case-truth", "hostility-proxy-control"):  # not services: variants and listeners, checked through their probes
            continue
        actual = set(config["services"][name].get("networks") or {})
        status = "ok" if actual == expected else "MISMATCH"
        print(f"  {name:18} {sorted(actual)} {status}")
        if actual != expected:
            fail(f"{name} is on {sorted(actual)}, PERD §65.1 says {sorted(expected)}")
    for probe, service in (("probe-core", "core"), ("probe-model", "model"), ("probe-ui", "ui"), ("probe-model-no-egress", "model-no-egress")):
        if set(config["services"][probe]["networks"]) != NETWORKS[service]:
            fail(f"{probe} does not carry the networks of {service}")
    print("   routes out (non-internal networks) per service:")
    for service, expected in EGRESS_NETWORKS.items():
        actual = {n for n in (config["services"][service].get("networks") or {}) if not config["networks"][n].get("internal")}
        status = "ok" if actual == expected else "MISMATCH"
        print(f"  {service:18} {sorted(actual) or 'none'} {status}")
        if actual != expected:
            fail(f"{service} has routes out through {sorted(actual)}, PERD §65.1 allows {sorted(expected)}")
    for service in ("core", "model", "postgres", "temporal"):
        if config["services"][service].get("ports"):
            fail(f"{service} publishes a port; only the UI may")
    for net, spec in config["networks"].items():
        if net == "bench_net" and any("bench_net" in (config["services"][s].get("networks") or {}) for s in ("core", "model")):
            fail("bench_net is reachable from core or model")
    for net in ("edge_net", "svc_net", "data_net", "tushe_net", "bench_net"):
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


def host_listener() -> tuple[socket.socket, int]:
    """A listener on the host. From a container it is an address outside the stack: reachable only
    through a network that is not Docker-internal."""
    server = socket.socket()
    server.bind(("0.0.0.0", 0))
    server.listen(16)

    def accept() -> None:
        while True:
            try:
                server.accept()[0].close()
            except OSError:
                return

    threading.Thread(target=accept, daemon=True).start()
    return server, server.getsockname()[1]


def check_egress() -> None:
    print("5. egress: can each probe reach an address outside the stack?")
    server, port = host_listener()
    target = f"host.docker.internal:{port}"
    try:
        for service, may in OUTSIDE_EXPECTED.items():
            out = run(COMPOSE + ["run", "--rm", "--no-deps", "-T", f"probe-{service}", "python", "/probe.py", target])
            try:
                got = json.loads(out.stdout.strip().splitlines()[-1])[target] == "open"
            except (ValueError, IndexError, KeyError):
                fail(f"probe-{service} gave no egress result: {out.stderr.strip()[:200]}")
                continue
            note = {"core": "no route out", "model-no-egress": "model minus provider_egress: no route out", "model": "through provider_egress only", "ui": "through ui_publish_net"}[service]
            print(f"  {service:16} outside {'reachable' if got else 'refused':9} ({note})")
            if got != may:
                fail(f"{service} {'can reach' if got else 'cannot reach'} an outside address; expected {'reachable' if may else 'refused'}")
    finally:
        server.close()
    try:
        status = urllib.request.urlopen("http://127.0.0.1:8082/healthz", timeout=5).status
    except OSError as exc:
        status = f"error {exc}"
    print(f"  UI on its published port 127.0.0.1:8082/healthz: {status}")
    if status != 200:
        fail(f"the UI does not serve on its published port: {status}")


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


# What the identity probe must see on mock-case (REQ-ENV-002, REQ-ENV-008): the bench credential opens the truth
# endpoint, and every runtime identity (the case adapter's credential, no credential, a guessed one) is refused there.
MOCK_CASE_EXPECTED = {
    "truth_with_bench_credential": 200,
    "truth_with_runtime_credential": 403,
    "truth_without_credential": 401,
    "truth_with_guessed_credential": 401,
    "reset_with_runtime_credential": 403,
    "fault_plan_with_runtime_credential": 403,
    "case_api_with_bench_credential": 403,
    "case_api_with_runtime_credential": 200,
    "case_api_without_credential": 401,
    "case_health_is_open": 200,
}
MOCK_CASE_SERVICES = ["mock-case", "mock-case-db", "mock-case-credentials", "probe-bench"]


def check_mock_case() -> None:
    print("6. mock-case: the truth endpoint opens for the bench credential and refuses every runtime identity")
    out = run(COMPOSE + ["run", "--rm", "--no-deps", "-T", "probe-bench", "python", "/mock_case_probe.py"])
    try:
        seen = json.loads(out.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        fail(f"probe-bench gave no result: {out.stderr.strip()[:200]}")
        return
    for name, want in MOCK_CASE_EXPECTED.items():
        got = seen.get(name)
        print(f"  {name:36} {got} {'ok' if got == want else f'!= {want}'}")
        if got != want:
            fail(f"mock-case {name}: expected {want}, saw {got}")


HOSTILITY_PROXY_SERVICES = ["hostility-proxy", "hostility-proxy-anchor", "hostility-proxy-credentials"]
CONTROL_PROBE = (
    "import urllib.request, urllib.error, json\n"
    "def code(h):\n"
    "    r = urllib.request.Request('http://hostility-proxy:8081/control/v1/fault-plan', headers=h)\n"
    "    try: return urllib.request.urlopen(r, timeout=5).status\n"
    "    except urllib.error.HTTPError as e: return e.code\n"
    "print(json.dumps({'no_credential': code({}), 'wrong_credential': code({'Authorization': 'Bearer wrong'})}))\n"
)


def check_hostility_proxy() -> None:
    """Rows for the hostility proxy (T4 item 7): absent from the dev profile; its control API answers a caller on
    bench_net and refuses it (401, logged) without the bench credential. Its own rows; no other row is touched."""
    print("6b. hostility proxy (ci, bench and netcheck profiles only; REQ-ENV-005)")
    plain = ["docker", "compose", "-f", str(HERE.parent / "docker-compose.yml")]
    if "hostility-proxy" in json.loads(run(plain + ["--profile", "dev", "config", "--format", "json"]).stdout)["services"]:
        fail("hostility-proxy is present in the dev profile")
    out = run(COMPOSE + ["run", "--rm", "--no-deps", "-T", "probe-bench", "python", "-c", CONTROL_PROBE])
    try:
        seen = json.loads(out.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        fail(f"probe-bench gave no result for the hostility proxy: {out.stderr.strip()[:200]}")
        return
    for name, got in seen.items():
        print(f"  control API, {name:18} {got} {'ok' if got == 401 else '!= 401'}")
        if got != 401:
            fail(f"hostility proxy control API, {name}: expected 401, saw {got}")


def check_evil_provider() -> None:
    """Row for the evil provider (T4 item 9): bench profile only, on provider_egress only, no published port, reachable
    from the model's network and from nothing else. Its own rows; no other component's row is touched."""
    print("7. evil provider (bench profile only; ADV-08)")
    plain = ["docker", "compose", "-f", str(HERE.parent / "docker-compose.yml")]
    if "evil-provider" in json.loads(run(plain + ["--profile", "dev", "config", "--format", "json"]).stdout)["services"]:
        fail("evil-provider is present without the bench profile")
    bench = COMPOSE + ["--profile", "bench"]
    service = json.loads(run(bench + ["config", "--format", "json"]).stdout)["services"].get("evil-provider")
    if service is None:
        fail("evil-provider is missing from the bench profile")
        return
    networks = set(service.get("networks") or {})
    print(f"  evil-provider      {sorted(networks)} {'ok' if networks == {'provider_egress'} else 'MISMATCH'}")
    if networks != {"provider_egress"}:
        fail(f"evil-provider is on {sorted(networks)}, expected only provider_egress")
    if service.get("ports"):
        fail("evil-provider publishes a port")
    if set(service.get("profiles") or []) != {"bench"}:
        fail(f"evil-provider is in profiles {service.get('profiles')}, expected only bench")
    up = run(bench + ["up", "-d", "--wait", "--no-deps", "evil-provider"])
    if up.returncode != 0:
        fail(f"evil-provider did not start: {up.stderr.strip()[:200]}")
        return
    try:
        target = "evil-provider:8090"
        for probe, may in (("probe-core", False), ("probe-ui", False), ("probe-model", True)):
            out = run(bench + ["run", "--rm", "--no-deps", "-T", probe, "python", "/probe.py", target])
            try:
                got = json.loads(out.stdout.strip().splitlines()[-1])[target]
            except (ValueError, IndexError, KeyError):
                fail(f"{probe} gave no result for the evil provider: {out.stderr.strip()[:200]}")
                continue
            want = "open" if may else "refused"
            print(f"  {probe:18} -> evil-provider {got} (expected {want})")
            if got != want:
                fail(f"{probe} -> evil-provider: expected {want}, saw {got}")
    finally:
        run(bench + ["rm", "-sf", "evil-provider"])


RELAY_PROBE = (
    "import urllib.request, urllib.error, json\n"
    "def call(method, path, headers=None, body=None):\n"
    "    r = urllib.request.Request('http://webhook-relay:8090' + path, data=body, method=method, headers=headers or {})\n"
    "    try:\n"
    "        with urllib.request.urlopen(r, timeout=5) as x: return x.status, x.read().decode()\n"
    "    except urllib.error.HTTPError as e: return e.code, e.read().decode()\n"
    "out = {}\n"
    "out['pull_without_credential'] = call('GET', '/v1/deliveries')\n"
    "out['pull_with_wrong_credential'] = call('GET', '/v1/deliveries', {'Authorization': 'Bearer wrong'})\n"
    "out['stats_without_credential'] = call('GET', '/v1/stats')\n"
    "out['unsigned_delivery'] = call('POST', '/webhooks', {'Content-Type': 'application/json'}, b'{}')\n"
    "out['health_is_open'] = call('GET', '/healthz')\n"
    "print(json.dumps(out))\n"
)
RELAY_EXPECTED = {  # name -> status; the first three must also carry no delivery data
    "pull_without_credential": 401, "pull_with_wrong_credential": 401, "stats_without_credential": 401,
    "unsigned_delivery": 400, "health_is_open": 200,
}


def check_webhook_relay() -> None:
    """Identity rows for the webhook relay (ENV-13, DEC-2026-10-16 E6): in the running stack, a caller on tushe_net with
    no credential, or a wrong one, is refused the pull and the counters, and an unsigned delivery is refused. Its own
    rows; no other component's row is touched."""
    print("8. webhook relay: a pull without the pull token is refused in the running stack")
    out = run(COMPOSE + ["run", "--rm", "--no-deps", "-T", "probe-core", "python", "-c", RELAY_PROBE])
    try:
        seen = json.loads(out.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        fail(f"probe-core gave no result for the webhook relay: {out.stderr.strip()[:200]}")
        return
    for name, want in RELAY_EXPECTED.items():
        status, body = seen.get(name, [None, ""])
        ok = status == want and (want != 401 or ('"deliveries"' not in body and '"counters"' not in body))
        print(f"  {name:28} {status} {'ok' if ok else f'!= {want} (or the refusal carried data)'}")
        if not ok:
            fail(f"webhook relay {name}: expected {want}, saw {status}")


# Per-profile runtime probes (PLT-04). WHAT each profile must start is checked statically against PERD §65.1 by profiles.py (SEC-11,
# noema#114), from `docker compose config`; keeping a second copy of that table here would let the two drift. This check does the part
# a static read cannot: it starts each run profile on its own and probes from the networks of core, model and ui against what
# REALLY runs, and requires that what runs is exactly what Compose says the profile is.
DEV_SERVICES = {"core", "model", "ui", "postgres", "temporal"}
LIVE_PROFILES = ("ci", "bench", "demo")
ONE_SHOT = ("-credentials", "-anchor")  # sidecars that may have exited by the time the stack is up; never required to be running
# the compose service behind each probe target (the truth and control listeners belong to the container that serves them)
SERVICE_OF = {"mock-case-truth": "mock-case", "hostility-proxy-control": "hostility-proxy"}
PROFILE_TARGETS = [t for t in TARGETS if not t.startswith("sentinel-")]  # the sentinels belong to the netcheck profile, not to a run profile


def live_expectation(source: str, target: str, running: set) -> str:
    """What a probe carrying `source`'s networks must see at `target` while exactly `running` runs: open only when the target's
    service runs and a network is shared. Pure: tested offline."""
    service = SERVICE_OF.get(target, target)
    return "open" if service in running and NETWORKS[source] & NETWORKS[target] else "refused"


def live_findings(profile: str, declared: set, running: set) -> list[str]:
    """What is wrong with the set of services that really runs: anything running that Compose does not declare for the profile, or a
    declared service that should be running and is not. Pure: tested offline."""
    found = [f"{profile}: {name} is running but is not part of this profile" for name in sorted(running - declared)]
    required = {n for n in declared if not n.endswith(ONE_SHOT)}
    found += [f"{profile}: {name} should be running and is not" for name in sorted(required - running)]
    return found


def check_profiles() -> None:
    print("9. each run profile started on its own, probed from core, model and ui (ci, bench, demo)")
    plain = ["docker", "compose", "-f", str(HERE.parent / "docker-compose.yml")]
    for profile in LIVE_PROFILES:
        check_profile_live(profile, plain)


def check_profile_live(profile: str, plain: list) -> None:
    """Starts one profile on its own (never with netcheck, which would start everything), requires that exactly its services run,
    and repeats the probes of check 2 from the networks of core, model and ui against what really runs."""
    run_profile = plain + ["--profile", profile]
    with_probes = run_profile + ["--profile", "netcheck"]
    declared = set(run(run_profile + ["config", "--services"]).stdout.split())
    running: set = set()
    up = run(run_profile + ["up", "-d", "--wait"])
    if up.returncode != 0:
        fail(f"{profile}: the profile did not start: {up.stderr.strip()[-300:]}")
        run(run_profile + ["down", "--remove-orphans"])
        return
    try:
        running = set(run(run_profile + ["ps", "--services", "--status", "running"]).stdout.split())
        for f in live_findings(profile, declared, running):
            fail(f)
        targets = [TARGETS[t] for t in PROFILE_TARGETS]
        for source in ("core", "model", "ui"):
            out = run(with_probes + ["run", "--rm", "--no-deps", "-T", f"probe-{source}", "python", "/probe.py"] + targets)
            try:
                seen = json.loads(out.stdout.strip().splitlines()[-1])
            except (ValueError, IndexError):
                fail(f"{profile}: probe-{source} gave no result: {out.stderr.strip()[:200]}")
                continue
            cells = []
            for target in PROFILE_TARGETS:
                if target == source:
                    continue
                want, got = live_expectation(source, target, running), seen.get(TARGETS[target], "none")
                cells.append(f"{target}={got}" if got == want else f"{target}={got}!={want}")
                if got != want:
                    fail(f"{profile}: {source} -> {target}: expected {want}, saw {got}")
            print(f"  {profile:6} from {source:5} " + " ".join(cells))
    finally:
        extras = sorted(running - DEV_SERVICES)
        if extras:
            run(run_profile + ["rm", "-sf"] + extras)


def main() -> int:
    up = run(COMPOSE + ["up", "-d", "--wait", "sentinel-tushe", "sentinel-provider", "sentinel-bench", "mock-case", "hostility-proxy", "webhook-relay"])
    if up.returncode != 0:
        print(up.stderr)
        return 2
    check_membership()
    observed = check_matrix()
    check_ten01(observed)
    check_real_model()
    check_database()
    check_egress()
    check_mock_case()
    check_hostility_proxy()
    check_evil_provider()
    check_webhook_relay()
    run(COMPOSE + ["rm", "-sf", "sentinel-tushe", "sentinel-provider", "sentinel-bench"] + MOCK_CASE_SERVICES + HOSTILITY_PROXY_SERVICES + ["webhook-relay", "webhook-relay-credentials"])
    check_profiles()
    if failures:
        print(f"\nmatrix FAIL: {len(failures)} mismatches")
        return 1
    print(f"\nmatrix OK: {len(MUST_BE_REFUSED)} forbidden paths refused, core and model-without-egress cannot reach outside, every other path as PERD §65.1 allows")
    return 0


if __name__ == "__main__":
    sys.exit(main())
