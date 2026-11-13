#!/usr/bin/env python3
"""Static topology check of every Compose run profile (SEC-11; REQ-OPS-001, REQ-SEC-002, REQ-ENV-002, PERD section 65.1).

The runtime matrix (matrix.py) probes a running stack under the dev profile. The stack that runs the benchmark and the demo is
a different set of services (ci, bench, demo), so this check reads the resolved configuration of each profile with
`docker compose config` (no daemon needed) and requires, for every service the profile builds:

  1. exactly the networks of PERD section 65.1 (written out here, independently of the Compose file); an unknown service fails,
     so a new service has to be added to this table on purpose;
  2. the oracle side (bench_net) is held only by mock-case, its database, the hostility proxy and its anchor, and the stand-in and its anchor; the core, the model and
     the UI are never on it, and nothing but the model and the evil provider is on provider_egress;
  3. only the UI and the webhook relay publish a port, and only on 127.0.0.1;
  4. no privileged container, host network or PID/IPC namespace, added capability, or Docker socket mount;
  5. the networks that are not Docker-internal are exactly provider_egress, ui_publish_net and tushe_egress;
  6. profile placement: the evil provider only in bench; the hostility proxy and the stand-in only in ci and bench; the webhook relay only in ci;
  7. the exact set of services each profile builds: a missing or an extra service fails.

`--self-test` mutates a resolved configuration and requires that each rule fires. Exit status 1 on any failure. Standard library only.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

COMPOSE_FILE = Path(__file__).resolve().parent.parent / "docker-compose.yml"
PROFILES = ("dev", "ci", "bench", "demo")

# PERD section 65.1, independent of the Compose file. network_mode "none" services (credential sidecars) have no networks.
NETWORKS = {
    "ui": {"edge_net", "ui_publish_net"},
    "core": {"edge_net", "svc_net", "data_net", "tushe_net"},
    "model": {"edge_net", "svc_net", "provider_egress"},
    "postgres": {"data_net"},
    "temporal": {"data_net"},
    "mock-case": {"tushe_net", "bench_net"},
    "mock-case-db": {"bench_net"},
    "hostility-proxy": {"tushe_net", "bench_net", "tushe_egress"},
    "hostility-proxy-anchor": {"bench_net"},
    "standin": {"tushe_net", "bench_net"},
    "standin-anchor": {"bench_net"},
    "bench-runner": {"bench_net", "edge_net"},
    "webhook-relay": {"tushe_net", "tushe_egress"},
    "evil-provider": {"provider_egress"},
    "mock-case-credentials": set(),
    "hostility-proxy-credentials": set(),
    "standin-credentials": set(),
    "webhook-relay-credentials": set(),
}
SIDECARS = {"mock-case-credentials", "hostility-proxy-credentials", "webhook-relay-credentials", "standin-credentials"}
ORACLE_SIDE = {"mock-case", "mock-case-db", "hostility-proxy", "hostility-proxy-anchor", "standin", "standin-anchor", "bench-runner"}
PROVIDER_SIDE = {"model", "evil-provider"}
# The volumes that hold a bench credential: only the oracle side (and the sidecars that write them) may mount them. The core in particular holds the stand-in's runtime
# credential and principal key, never a bench credential (REQ-ENV-002: the key that loads never reads).
BENCH_CREDENTIAL_VOLUMES = {"mock_case_bench_credential", "standin_bench_credential", "hostility_proxy_credentials"}
PUBLISHED = {"ui": {("127.0.0.1", "8082")}, "webhook-relay": {("127.0.0.1", "8090")}}
EXTERNAL = {"provider_egress", "ui_publish_net", "tushe_egress"}
# The services each profile must build, written out here (PERD section 65.1). A missing service fails, as does an extra one: a profile
# that silently loses the core or the model, or gains a service, is a change somebody must make on purpose in this table too.
EXPECTED_SERVICES = {
    "dev": {"core", "model", "postgres", "temporal", "ui"},
    "ci": {"core", "model", "postgres", "temporal", "ui", "mock-case", "mock-case-db", "mock-case-credentials", "hostility-proxy",
           "hostility-proxy-anchor", "hostility-proxy-credentials", "webhook-relay", "webhook-relay-credentials", "standin", "standin-anchor", "standin-credentials"},
    "bench": {"core", "model", "postgres", "temporal", "ui", "mock-case", "mock-case-db", "mock-case-credentials", "hostility-proxy",
              "hostility-proxy-anchor", "hostility-proxy-credentials", "evil-provider", "standin", "standin-anchor", "standin-credentials", "bench-runner"},
    "demo": {"core", "model", "postgres", "temporal", "ui", "mock-case", "mock-case-db", "mock-case-credentials"},
}
ONLY_IN = {"evil-provider": {"bench"}, "hostility-proxy": {"ci", "bench"}, "hostility-proxy-anchor": {"ci", "bench"},
           "hostility-proxy-credentials": {"ci", "bench"}, "webhook-relay": {"ci"}, "webhook-relay-credentials": {"ci"},
           "standin": {"ci", "bench"}, "standin-anchor": {"ci", "bench"}, "standin-credentials": {"ci", "bench"}, "bench-runner": {"bench"}}


# The bench stack is started with these overlays (make bench-up). Their placeholder variables are set here only so `config` can resolve them.
BENCH_OVERLAYS = ("bench-model.yml", "bench-stack.yml")
BENCH_ENV = {"NOEMA_BENCH_OUT": "/tmp/noema-bench-out", "NOEMA_BENCH_UID": "1000", "NOEMA_BENCH_GID": "1000"}


def resolved(profile):
    files = [str(COMPOSE_FILE)] + ([str(COMPOSE_FILE.parent / f) for f in BENCH_OVERLAYS] if profile == "bench" else [])
    cmd = ["docker", "compose"] + [a for f in files for a in ("-f", f)] + ["--profile", profile, "config", "--format", "json"]
    out = subprocess.run(cmd, capture_output=True, text=True, env={**os.environ, **BENCH_ENV} if profile == "bench" else None)
    if out.returncode != 0:
        raise SystemExit(f"docker compose config failed for profile {profile}: {out.stderr.strip()[:300]}")
    return json.loads(out.stdout)


def check(cfg, profile):
    """Returns the list of violations for one resolved profile configuration."""
    bad = []
    have, want = set(cfg["services"]), EXPECTED_SERVICES[profile]
    for name in sorted(want - have):
        bad.append(f"{profile}: the service {name!r} is missing; the profile must build exactly {sorted(want)}")
    for name in sorted(have - want):
        bad.append(f"{profile}: the service {name!r} is not expected in this profile")
    for name, svc in sorted(cfg["services"].items()):
        if name not in NETWORKS:
            bad.append(f"{profile}: unknown service {name!r}: add it to profiles.py with its PERD 65.1 networks on purpose")
            continue
        nets = set((svc.get("networks") or {}).keys())
        if nets != NETWORKS[name]:
            bad.append(f"{profile}: {name} is on {sorted(nets)}, PERD 65.1 says {sorted(NETWORKS[name])}")
        if name in SIDECARS and svc.get("network_mode") != "none":
            bad.append(f"{profile}: {name} must have network_mode none, has {svc.get('network_mode')!r}")
        if name not in SIDECARS and svc.get("network_mode"):
            bad.append(f"{profile}: {name} sets network_mode {svc['network_mode']!r}")
        if name not in ORACLE_SIDE and "bench_net" in nets:
            bad.append(f"{profile}: {name} is on bench_net (oracle side)")
        if name not in PROVIDER_SIDE and "provider_egress" in nets:
            bad.append(f"{profile}: {name} is on provider_egress")
        published = {(p.get("host_ip") or "0.0.0.0", str(p.get("published"))) for p in svc.get("ports", [])}
        if published != PUBLISHED.get(name, set()):
            bad.append(f"{profile}: {name} publishes {sorted(published)}, expected {sorted(PUBLISHED.get(name, set()))}")
        for flag in ("privileged", "cap_add", "devices"):
            if svc.get(flag):
                bad.append(f"{profile}: {name} sets {flag}")
        for flag in ("pid", "ipc", "userns_mode"):
            if svc.get(flag) == "host":
                bad.append(f"{profile}: {name} uses the host {flag} namespace")
        for vol in svc.get("volumes", []) or []:
            if "docker.sock" in str(vol.get("source", "")):
                bad.append(f"{profile}: {name} mounts the Docker socket")
            if vol.get("source") in BENCH_CREDENTIAL_VOLUMES and name not in ORACLE_SIDE and name not in SIDECARS:
                bad.append(f"{profile}: {name} mounts the bench credential volume {vol['source']} (oracle side only)")
            if name == "model":
                target, source = str(vol.get("target", "")), str(vol.get("source", ""))
                # The model gets the benchmark COST directory and nothing else of the output directory: the runner writes the generated suite and its oracle documents beside it
                # (INV-22; Codex P1 on noema#444). So no model mount may be, contain or sit under /bench-out except /bench-out/cost, whose host source is the cost directory itself.
                if target == "/bench-out" or target.startswith("/bench-out/") and target != "/bench-out/cost" or "/bench-out".startswith(target.rstrip("/") + "/") or target == "/":
                    bad.append(f"{profile}: the model mounts {target!r}: only /bench-out/cost may be shared with it (the suite and the oracle live beside it)")
                if target == "/bench-out/cost" and not source.replace("\\", "/").rstrip("/").endswith("/cost"):
                    bad.append(f"{profile}: the model's /bench-out/cost comes from {source!r}, not from a directory named cost")
        if name in ONLY_IN and profile not in ONLY_IN[name]:
            bad.append(f"{profile}: {name} is present but is allowed only in {sorted(ONLY_IN[name])}")
    for net, spec in cfg.get("networks", {}).items():
        internal = bool(spec.get("internal"))
        if (net in EXTERNAL) == internal:
            bad.append(f"{profile}: network {net} is {'internal' if internal else 'external'}; "
                       f"{'external' if net in EXTERNAL else 'internal'} expected")
    return bad


def self_test(configs):
    """Each mutation must make check() report something; a rule that cannot fail proves nothing. `configs` maps profile to its resolved config."""
    import copy
    dev, ci, bench, demo = configs["dev"], configs["ci"], configs["bench"], configs["demo"]
    sidecar = {"network_mode": "none"}
    cases = {  # label -> (profile, mutation applied to a copy of that profile's configuration)
        "core joins bench_net (oracle)": ("dev", lambda c: c["services"]["core"]["networks"].update({"bench_net": None})),
        "model leaves svc_net": ("dev", lambda c: c["services"]["model"]["networks"].pop("svc_net")),
        "unknown service": ("dev", lambda c: c["services"].update({"extra": {"networks": {"edge_net": None}}})),
        "postgres publishes a port": ("dev", lambda c: c["services"]["postgres"].update({"ports": [{"host_ip": "127.0.0.1", "published": "5432"}]})),
        "ui binds all interfaces": ("dev", lambda c: c["services"]["ui"].update({"ports": [{"published": "8082"}]})),
        "core is privileged": ("dev", lambda c: c["services"]["core"].update({"privileged": True})),
        "core uses host network": ("dev", lambda c: c["services"]["core"].update({"network_mode": "host"})),
        "core mounts docker.sock": ("dev", lambda c: c["services"]["core"].update({"volumes": [{"source": "/var/run/docker.sock"}]})),
        "tushe_net becomes external": ("dev", lambda c: c["networks"]["tushe_net"].pop("internal")),
        "data_net becomes external": ("dev", lambda c: c["networks"]["data_net"].pop("internal")),
        "ui_publish_net becomes internal": ("dev", lambda c: c["networks"]["ui_publish_net"].update({"internal": True})),
        # required service sets
        "dev loses the core": ("dev", lambda c: c["services"].pop("core")),
        "dev loses the model": ("dev", lambda c: c["services"].pop("model")),
        "ci loses the webhook relay": ("ci", lambda c: c["services"].pop("webhook-relay")),
        "bench loses the evil provider": ("bench", lambda c: c["services"].pop("evil-provider")),
        "demo loses mock-case": ("demo", lambda c: c["services"].pop("mock-case")),
        # profile placement, one case per ONLY_IN rule
        "the evil provider appears in dev": ("dev", lambda c: c["services"].update({"evil-provider": {"networks": {"provider_egress": None}}})),
        "the evil provider appears in ci": ("ci", lambda c: c["services"].update({"evil-provider": {"networks": {"provider_egress": None}}})),
        "the evil provider appears in demo": ("demo", lambda c: c["services"].update({"evil-provider": {"networks": {"provider_egress": None}}})),
        "the hostility proxy appears in demo": ("demo", lambda c: c["services"].update({"hostility-proxy": {"networks": {"tushe_net": None, "bench_net": None, "tushe_egress": None}}})),
        "the hostility proxy appears in dev": ("dev", lambda c: c["services"].update({"hostility-proxy": {"networks": {"tushe_net": None, "bench_net": None, "tushe_egress": None}}})),
        "the hostility proxy anchor appears in demo": ("demo", lambda c: c["services"].update({"hostility-proxy-anchor": {"networks": {"bench_net": None}}})),
        "the hostility proxy credentials appear in dev": ("dev", lambda c: c["services"].update({"hostility-proxy-credentials": dict(sidecar)})),
        "the webhook relay appears in bench": ("bench", lambda c: c["services"].update({"webhook-relay": {"networks": {"tushe_net": None, "tushe_egress": None}, "ports": [{"host_ip": "127.0.0.1", "published": "8090"}]}})),
        "the webhook relay appears in demo": ("demo", lambda c: c["services"].update({"webhook-relay": {"networks": {"tushe_net": None, "tushe_egress": None}, "ports": [{"host_ip": "127.0.0.1", "published": "8090"}]}})),
        "the webhook relay credentials appear in bench": ("bench", lambda c: c["services"].update({"webhook-relay-credentials": dict(sidecar)})),
        "the stand-in appears in dev": ("dev", lambda c: c["services"].update({"standin": {"networks": {"tushe_net": None, "bench_net": None}}})),
        "the stand-in appears in demo": ("demo", lambda c: c["services"].update({"standin": {"networks": {"tushe_net": None, "bench_net": None}}})),
        "the stand-in anchor appears in demo": ("demo", lambda c: c["services"].update({"standin-anchor": {"networks": {"bench_net": None}}})),
        "the stand-in credentials appear in dev": ("dev", lambda c: c["services"].update({"standin-credentials": dict(sidecar)})),
        "the bench runner appears in dev": ("dev", lambda c: c["services"].update({"bench-runner": {"networks": {"bench_net": None, "edge_net": None}}})),
        "the bench runner appears in ci": ("ci", lambda c: c["services"].update({"bench-runner": {"networks": {"bench_net": None, "edge_net": None}}})),
        "the bench runner appears in demo": ("demo", lambda c: c["services"].update({"bench-runner": {"networks": {"bench_net": None, "edge_net": None}}})),
        "the bench runner leaves edge_net": ("bench", lambda c: c["services"]["bench-runner"]["networks"].pop("edge_net")),
        "the bench runner joins tushe_net": ("bench", lambda c: c["services"]["bench-runner"]["networks"].update({"tushe_net": None})),
        "the bench runner joins provider_egress": ("bench", lambda c: c["services"]["bench-runner"]["networks"].update({"provider_egress": None})),
        "the bench runner publishes a port": ("bench", lambda c: c["services"]["bench-runner"].update({"ports": [{"host_ip": "127.0.0.1", "published": "9000"}]})),
        "the bench runner mounts docker.sock": ("bench", lambda c: c["services"]["bench-runner"].update({"volumes": [{"source": "/var/run/docker.sock"}]})),
        "the core mounts the stand-in bench credential": ("bench", lambda c: c["services"]["core"].setdefault("volumes", []).append({"source": "standin_bench_credential", "target": "/x"})),
        "the model mounts the mock-case bench credential": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"source": "mock_case_bench_credential", "target": "/x"})),
        "the model mounts the whole bench output directory": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out", "target": "/bench-out"})),
        "the model mounts the generated suite": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/suite", "target": "/bench-out/suite"})),
        "the model mounts the oracle directory": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/suite/oracle", "target": "/bench-out/suite/oracle"})),
        "the model mounts the filesystem root": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/", "target": "/"})),
        "the model's cost mount is the whole output directory": ("bench", lambda c: [v.update({"source": "/tmp/noema-bench-out"}) for v in c["services"]["model"]["volumes"] if v.get("target") == "/bench-out/cost"]),
        "the stand-in leaves bench_net": ("ci", lambda c: c["services"]["standin"]["networks"].pop("bench_net")),
    }
    missed = []
    for label, (profile, mutate) in cases.items():
        mutated = copy.deepcopy(configs[profile])
        mutate(mutated)
        if not check(mutated, profile):
            missed.append(label)
    # And the unmutated configurations must be clean, so a firing rule is the mutation and not noise.
    for profile, cfg in configs.items():
        if check(cfg, profile):
            missed.append(f"unmutated {profile} is not clean")
    return missed, len(cases)


def main():
    failures = []
    configs = {}
    for profile in PROFILES:
        cfg = configs[profile] = resolved(profile)
        found = check(cfg, profile)
        print(f"{'FAIL' if found else 'ok  '} profile {profile:6} services: {', '.join(sorted(cfg['services']))}")
        failures += found
    if "--self-test" in sys.argv:
        missed, n = self_test(configs)
        print(f"self-test: {n} mutations;", "every one is caught" if not missed else "NOT CAUGHT: " + "; ".join(missed))
        failures += [f"self-test: rule did not fire for: {m}" for m in missed]
    for f in failures:
        print("  -", f)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
