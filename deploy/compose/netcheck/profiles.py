#!/usr/bin/env python3
"""Static topology check of every Compose run profile (SEC-11; REQ-OPS-001, REQ-SEC-002, REQ-ENV-002, PERD section 65.1).

The runtime matrix (matrix.py) probes a running stack under the dev profile. The stack that runs the benchmark and the demo is
a different set of services (ci, bench, demo), so this check reads the resolved configuration of each profile with
`docker compose config` (no daemon needed) and requires, for every service the profile builds:

  1. exactly the networks of PERD section 65.1 (written out here, independently of the Compose file); an unknown service fails,
     so a new service has to be added to this table on purpose;
  2. the oracle side (bench_net) is held only by mock-case, its database, the hostility proxy and its anchor; the core, the model and
     the UI are never on it, and nothing but the model and the evil provider is on provider_egress;
  3. only the UI and the webhook relay publish a port, and only on 127.0.0.1;
  4. no privileged container, host network or PID/IPC namespace, added capability, or Docker socket mount;
  5. the networks that are not Docker-internal are exactly provider_egress, ui_publish_net and tushe_egress;
  6. profile placement: the evil provider only in bench; the hostility proxy only in ci and bench; the webhook relay only in ci.

`--self-test` mutates a resolved configuration and requires that each rule fires. Exit status 1 on any failure. Standard library only.
"""
import json
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
    "webhook-relay": {"tushe_net", "tushe_egress"},
    "evil-provider": {"provider_egress"},
    "mock-case-credentials": set(),
    "hostility-proxy-credentials": set(),
    "webhook-relay-credentials": set(),
}
SIDECARS = {"mock-case-credentials", "hostility-proxy-credentials", "webhook-relay-credentials"}
ORACLE_SIDE = {"mock-case", "mock-case-db", "hostility-proxy", "hostility-proxy-anchor"}
PROVIDER_SIDE = {"model", "evil-provider"}
PUBLISHED = {"ui": {("127.0.0.1", "8082")}, "webhook-relay": {("127.0.0.1", "8090")}}
EXTERNAL = {"provider_egress", "ui_publish_net", "tushe_egress"}
ONLY_IN = {"evil-provider": {"bench"}, "hostility-proxy": {"ci", "bench"}, "hostility-proxy-anchor": {"ci", "bench"},
           "hostility-proxy-credentials": {"ci", "bench"}, "webhook-relay": {"ci"}, "webhook-relay-credentials": {"ci"}}


def resolved(profile):
    out = subprocess.run(["docker", "compose", "-f", str(COMPOSE_FILE), "--profile", profile, "config", "--format", "json"],
                         capture_output=True, text=True)
    if out.returncode != 0:
        raise SystemExit(f"docker compose config failed for profile {profile}: {out.stderr.strip()[:300]}")
    return json.loads(out.stdout)


def check(cfg, profile):
    """Returns the list of violations for one resolved profile configuration."""
    bad = []
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
        if name in ONLY_IN and profile not in ONLY_IN[name]:
            bad.append(f"{profile}: {name} is present but is allowed only in {sorted(ONLY_IN[name])}")
    for net, spec in cfg.get("networks", {}).items():
        internal = bool(spec.get("internal"))
        if (net in EXTERNAL) == internal:
            bad.append(f"{profile}: network {net} is {'internal' if internal else 'external'}; "
                       f"{'external' if net in EXTERNAL else 'internal'} expected")
    return bad


def self_test(cfg):
    """Each mutation must make check() report something; a rule that cannot fail proves nothing."""
    import copy
    cases = {
        "core joins bench_net (oracle)": lambda c: c["services"]["core"]["networks"].update({"bench_net": None}),
        "model leaves svc_net": lambda c: c["services"]["model"]["networks"].pop("svc_net"),
        "unknown service": lambda c: c["services"].update({"extra": {"networks": {"edge_net": None}}}),
        "postgres publishes a port": lambda c: c["services"]["postgres"].update({"ports": [{"host_ip": "127.0.0.1", "published": "5432"}]}),
        "ui binds all interfaces": lambda c: c["services"]["ui"].update({"ports": [{"published": "8082"}]}),
        "core is privileged": lambda c: c["services"]["core"].update({"privileged": True}),
        "core uses host network": lambda c: c["services"]["core"].update({"network_mode": "host"}),
        "core mounts docker.sock": lambda c: c["services"]["core"].update({"volumes": [{"source": "/var/run/docker.sock"}]}),
        "tushe_net becomes external": lambda c: c["networks"]["tushe_net"].pop("internal"),
        "data_net becomes external": lambda c: c["networks"]["data_net"].pop("internal"),
        "ui_publish_net becomes internal": lambda c: c["networks"]["ui_publish_net"].update({"internal": True}),
    }
    missed = []
    for label, mutate in cases.items():
        mutated = copy.deepcopy(cfg)
        mutate(mutated)
        if not check(mutated, "dev"):
            missed.append(label)
    return missed


def main():
    failures = []
    for profile in PROFILES:
        cfg = resolved(profile)
        found = check(cfg, profile)
        print(f"{'FAIL' if found else 'ok  '} profile {profile:6} services: {', '.join(sorted(cfg['services']))}")
        failures += found
    if "--self-test" in sys.argv:
        missed = self_test(resolved("dev"))
        print("self-test:", "every rule fires" if not missed else "RULES THAT DID NOT FIRE: " + "; ".join(missed))
        failures += [f"self-test: rule did not fire for: {m}" for m in missed]
    for f in failures:
        print("  -", f)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
