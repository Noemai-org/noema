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
  7. the exact set of services each profile builds: a missing or an extra service fails;
  8. provider credentials live only in the model service (INV-29): an environment variable or Compose secret named like a provider key or endpoint (`*_API_KEY`, `INTRON_*`,
     `OPENAI_*`, ...) on any other service fails, and the provider side (model, evil provider) holds no database, Temporal or grant-key setting. Names only: no value is read.

`--self-test` mutates a resolved configuration and requires that each rule fires. Exit status 1 on any failure. Standard library only.
"""
import json
import os
import re
import subprocess
import sys
from pathlib import Path

COMPOSE_FILE = Path(__file__).resolve().parent.parent / "docker-compose.yml"
MODEL_DOCKERFILE = COMPOSE_FILE.parent.parent.parent / "services" / "model" / "Dockerfile"


SHELLS = {"sh", "bash", "dash", "ash", "zsh", "ksh", "busybox"}


def _tokens(argv) -> list[str]:
    """Every word of a launch command, whether it is a list or one string, and whether one list element holds a whole command line (`["sh", "-c", "uvicorn ... --ssl-cert-reqs 0"]`).
    Elements are split on whitespace, `=` and quotes, so `--ssl-cert-reqs=0`, `'--ssl-cert-reqs' 0` and a word inside a shell string are all seen."""
    parts = argv.split() if isinstance(argv, str) else [str(a) for a in (argv or [])]
    return [t for part in parts for t in re.findall(r"[^\s='\"]+", part)]


def listener_flags(argv) -> list[str]:
    """The `--ssl-*` options in a launch command (a list, or one string; one list element may hold a whole command). uvicorn gives a command-line option precedence over its UVICORN_SSL_*
    variable, so one of these can switch off the client-certificate requirement while the variable still says 2: and the service's own startup check (`require_mtls_listener`, noema#635)
    reads only the variable. Codex and the Security Reviewer on noema#670: the shell-wrapped list form was not seen."""
    return [t for t in _tokens(argv) if t.startswith("--ssl-")]


def shell_wrapper(argv) -> str | None:
    """The shell the launch command runs through, if any. A shell can build an option string at run time (`sh -c "uvicorn $(...)"`), which no scan of the text can see; the model starts
    uvicorn directly, so a shell wrapper is refused outright (noema#670)."""
    toks = _tokens(argv)
    for t in toks[:1]:
        if t.rsplit("/", 1)[-1] in SHELLS:
            return t
    return None


def logical_instructions(text: str) -> list[str]:
    """The Dockerfile's instructions with line continuations joined (a backslash at the end of a line continues the instruction onto the next, and comment lines inside a continued
    instruction are dropped, as Docker does). A scan of physical lines would miss an `--ssl-*` option put on a continuation line (Codex P2 on noema#670)."""
    out: list[str] = []
    current = ""
    for raw in text.splitlines():
        line = raw.strip()
        if current and line.startswith("#"):
            continue  # a comment line inside a continued instruction
        if not current and (not line or line.startswith("#")):
            continue
        if line.endswith("\\"):
            current += line[:-1] + " "
            continue
        out.append((current + line).strip())
        current = ""
    if current:
        out.append(current.strip())
    return out


def env_assignments(instruction: str) -> list[tuple[str, str]]:
    """The (key, value) pairs one ENV instruction sets. Docker takes two forms (Codex P2 on noema#670): `ENV key=value [key=value ...]`, and the legacy `ENV key value`, whose value is
    everything after the first space. Surrounding quotes are removed from a value."""
    body = instruction.split(None, 1)[1] if len(instruction.split(None, 1)) == 2 else ""
    first = body.split(None, 1)[0] if body.split() else ""
    quotes = chr(34) + chr(39)
    if "=" not in first:
        parts = body.split(None, 1)
        return [(parts[0], parts[1].strip().strip(quotes))] if len(parts) == 2 else []
    return [(t.split("=", 1)[0], t.split("=", 1)[1].strip(quotes)) for t in body.split() if "=" in t]


def dockerfile_problems(text: str) -> list[str]:
    """The model image requires a client certificate and starts uvicorn with no --ssl-* option of its own (noema#555, noema#635): UVICORN_SSL_CERT_REQS is set, its LAST assignment is 2,
    and no ENTRYPOINT or CMD (continued over lines or not) carries an --ssl-* option. A parser directive that changes the escape character fails closed: the continuation rule above
    would no longer be the Dockerfile's."""
    bad = []
    if re.search(r"(?im)^\s*#\s*escape\s*=", text):
        bad.append("the model Dockerfile changes its escape character (# escape=): the checks of its instructions cannot be trusted; remove the directive")
    instructions = logical_instructions(text)
    assigned = [value for ln in instructions if ln.upper().startswith("ENV ") for key, value in env_assignments(ln) if key == "UVICORN_SSL_CERT_REQS"]
    if not assigned:
        bad.append("the model image does not set UVICORN_SSL_CERT_REQS=2")
    elif assigned[-1] != "2":
        bad.append(f"the model image's last assignment of UVICORN_SSL_CERT_REQS is {assigned[-1]!r}, not 2")
    for ln in instructions:
        if ln.upper().startswith(("ENTRYPOINT", "CMD")) and "--ssl-" in ln:
            bad.append(f"the model image starts uvicorn with an --ssl-* option, which overrides UVICORN_SSL_CERT_REQS: {ln[:80]}")
    return bad


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
    "grant-keys-init": set(),
    "trust-dsn-init": set(),
    "webhook-relay-credentials": set(),
}
SIDECARS = {"mock-case-credentials", "hostility-proxy-credentials", "webhook-relay-credentials", "standin-credentials", "grant-keys-init", "trust-dsn-init"}
ORACLE_SIDE = {"mock-case", "mock-case-db", "hostility-proxy", "hostility-proxy-anchor", "standin", "standin-anchor", "bench-runner"}
PROVIDER_SIDE = {"model", "evil-provider"}
# Provider credentials live only in the model service (INV-29, AGENT-05; A249 as relayed for Intron): an environment variable or a Compose secret whose name is a provider's key or
# endpoint setting is allowed on `model` and on no other service; and the provider side holds no credential of the core's database, Temporal or grants (PERD 41.1). Names only: a value
# is never read here. `evil-provider` is the synthetic provider of the bench and holds no real key, so it is not allowed one either.
# The prefixes are the providers of .env.example, the repository's inventory of secret and setting names (Codex P2 on noema#641: AZURE_FOUNDRY_ENDPOINT, OPENROUTER_ and TYPESAFE_ were missed);
# self_test() reads that file and fails when a name ending in _API_KEY or _ENDPOINT there is not classified as a provider name, so a new provider cannot be added unseen.
PROVIDER_NAME = re.compile(r"(?i)(^|_)(api_?key|secret_?key|access_?token|endpoint|api_base_url)$|^(intron|openai|aethex|indigenius|n_?atlas|anthropic|gemini|google|openrouter|azure_foundry|typesafe)_")
# Any name that holds a password (NOEMA_DB_PASSWORD_FILE, POSTGRES_PASSWORD_FILE, the Compose secret noema_core_password; Codex P1 on noema#641) is refused on the provider side: the model has no
# database login of any kind, so a password name there is wrong whatever it is called. (NOEMA_CORE_ADDR is not a credential: the model may be told where the core is.)
CORE_CREDENTIAL_NAME = re.compile(r"(?i)(dsn|database_url|password|passwd|pgpass|pguser|postgres_|temporal_|grant_key|signing_key)")
# The volumes that hold a bench credential: only the oracle side (and the sidecars that write them) may mount them. The core in particular holds the stand-in's runtime
# credential and principal key, never a bench credential (REQ-ENV-002: the key that loads never reads).
BENCH_CREDENTIAL_VOLUMES = {"mock_case_bench_credential", "standin_bench_credential", "hostility_proxy_credentials"}
PUBLISHED = {"ui": {("127.0.0.1", "8082")}, "webhook-relay": {("127.0.0.1", "8090")}}
EXTERNAL = {"provider_egress", "ui_publish_net", "tushe_egress"}
# The services each profile must build, written out here (PERD section 65.1). A missing service fails, as does an extra one: a profile
# that silently loses the core or the model, or gains a service, is a change somebody must make on purpose in this table too.
EXPECTED_SERVICES = {
    "dev": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init"},
    "ci": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "mock-case", "mock-case-db", "mock-case-credentials", "hostility-proxy",
           "hostility-proxy-anchor", "hostility-proxy-credentials", "webhook-relay", "webhook-relay-credentials", "standin", "standin-anchor", "standin-credentials"},
    "bench": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "mock-case", "mock-case-db", "mock-case-credentials", "hostility-proxy",
              "hostility-proxy-anchor", "hostility-proxy-credentials", "evil-provider", "standin", "standin-anchor", "standin-credentials", "bench-runner", "trust-dsn-init"},
    "demo": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "mock-case", "mock-case-db", "mock-case-credentials"},
}
ONLY_IN = {"evil-provider": {"bench"}, "hostility-proxy": {"ci", "bench"}, "hostility-proxy-anchor": {"ci", "bench"},
           "hostility-proxy-credentials": {"ci", "bench"}, "webhook-relay": {"ci"}, "webhook-relay-credentials": {"ci"},
           "standin": {"ci", "bench"}, "standin-anchor": {"ci", "bench"}, "standin-credentials": {"ci", "bench"}, "bench-runner": {"bench"}}


# The bench stack is started with these overlays (make bench-up). Their placeholder variables are set here only so `config` can resolve them.
# The mTLS material between the core and the model service (noema#555), from deploy/compose/tls_local.sh: each side mounts its own key and the CA certificate, never the other's key.
TLS_MOUNTS = {"core": {"ca.crt", "core-client.crt", "core-client.key"}, "model": {"ca.crt", "model.crt", "model.key", "model-health.crt", "model-health.key"},
              "temporal": {"ca.crt", "temporal.crt", "temporal.key"}}
TLS_REQUIRED = {"core": {"ca.crt", "core-client.crt", "core-client.key"}, "model": {"ca.crt", "model.crt", "model.key", "model-health.crt", "model-health.key"}}
# The settings of the core's client of the model service (noema#555, noema#563): the core reads exactly these five and refuses to build its client unless all five are set (no default,
# AGENT-16), so the Compose file must give the core every one. The names are the `Env...` constants of services/core/cmd/core/modelclient.go; main() compares the two when that file exists.
MODEL_CLIENT_SETTINGS = ("NOEMA_MODEL_URL", "NOEMA_MODEL_TLS_CA_FILE", "NOEMA_MODEL_TLS_CERT_FILE", "NOEMA_MODEL_TLS_KEY_FILE", "NOEMA_MODEL_MAX_RESPONSE_BYTES")
MODEL_CLIENT_SOURCE = Path(__file__).resolve().parents[3] / "services" / "core" / "cmd" / "core" / "modelclient.go"
# The Decision Grant HMAC keys (SEC-09; INV-29: the model holds no grant key). Two key files so a rotation has two active keys. Compose cannot set a file secret's mode or owner and the
# grant-key loader refuses a file that is not 0400 and process-owned, so the two secrets are held by the one-shot `grant-keys-init` step ONLY, under the names the loader reads; it writes
# them into the `grant_keys` volume, which the CORE mounts read-only and nothing else mounts (Codex and SR on noema#700). Names only.
GRANT_KEY_SECRETS = {"grant_hmac_key_a": "grant.hmac.1.key", "grant_hmac_key_b": "grant.hmac.2.key"}
GRANT_INIT, GRANT_VOLUME = "grant-keys-init", "grant_keys"
# A356 (interim, relayed): the bench core's settings with no default in code, the Architect's values, and the per-tenant Trust login DSN files. The tenant login password is a file secret held by
# postgres (init.sh makes the logins) and by the one-shot trust-dsn-init step ONLY; that step writes the DSN files into the trust_dsns volume, which the core mounts read-only and nothing else
# mounts. NOEMA_TRUST_AT_RISK_BPS is not here: trustlimits reads it from the pack. Changing a number is a ruling, not an edit (AGENT-16). Names and numbers only.
BENCH_CORE_SETTINGS = {
    "NOEMA_STORE_TIMEOUT_MS": "1000", "NOEMA_SEAL_TIMEOUT_MS": "1000", "NOEMA_COMPILE_TIMEOUT_MS": "3000", "NOEMA_READ_TIMEOUT_MS": "2000", "NOEMA_TRUST_DB_TIMEOUT_MS": "1000",
    "NOEMA_TRUST_RECONCILE_TIMEOUT_MS": "1000", "NOEMA_GRANT_KEY_MAX_ACTIVE": "2", "NOEMA_DB_SERIALIZATION_ATTEMPTS": "3", "NOEMA_OUTBOX_INTERVAL_MS": "1000",
    "NOEMA_GRANT_KEY_DIR": "/run/grant-keys", "NOEMA_PACK_DIR": "/run/packs", "NOEMA_TRUST_DSN_DIR": "/run/trust-dsn",
}
TRUST_DSN_INIT, TRUST_DSN_VOLUME, TENANT_LOGIN_SECRET = "trust-dsn-init", "trust_dsns", "noema_tenant_login_password"
# The environment of the auto-setup image that makes the Temporal frontend serve TLS (docker/config_template.yaml of the pinned server release: TEMPORAL_TLS_FRONTEND_CERT and _KEY are the
# server certificate, TEMPORAL_TLS_SERVER_CA_CERT the CA the in-process worker trusts, TEMPORAL_TLS_FRONTEND_SERVER_NAME the name it verifies) and the settings the image's CLI reads
# to reach it (TEMPORAL_TLS, TEMPORAL_TLS_CA, TEMPORAL_TLS_SERVER_NAME). In that template TEMPORAL_TLS_SERVER_CA_CERT is ALSO the root of the internode CLIENT, so setting it makes the
# services dial each other over TLS: the internode SERVER must then present a certificate (TEMPORAL_TLS_SERVER_CERT and _KEY) and the client must know the name to verify
# (TEMPORAL_TLS_INTERNODE_SERVER_NAME), or the services cannot reach each other (Codex P1 on noema#698). This is server-side TLS with NO client authentication (DEBT-46).
TEMPORAL_TLS_SETTINGS = ("TEMPORAL_TLS_FRONTEND_CERT", "TEMPORAL_TLS_FRONTEND_KEY", "TEMPORAL_TLS_SERVER_CA_CERT", "TEMPORAL_TLS_FRONTEND_SERVER_NAME", "TEMPORAL_TLS", "TEMPORAL_TLS_CA", "TEMPORAL_TLS_SERVER_NAME",
                         "TEMPORAL_TLS_SERVER_CERT", "TEMPORAL_TLS_SERVER_KEY", "TEMPORAL_TLS_INTERNODE_SERVER_NAME")
BENCH_OVERLAYS = ("bench-model.yml", "bench-stack.yml")
BENCH_ENV = {"NOEMA_BENCH_OUT": "/tmp/noema-bench-out", "NOEMA_BENCH_UID": "1000", "NOEMA_BENCH_GID": "1000", "NOEMA_BENCH_MAX_BODY_BYTES": "1024"}


INTRON_OVERLAY, INTRON_TARGET = "bench-intron.yml", "/run/intron/key"


def resolved(profile, with_intron=False):
    files = [str(COMPOSE_FILE)] + ([str(COMPOSE_FILE.parent / f) for f in BENCH_OVERLAYS] if profile == "bench" else [])
    if with_intron:
        files.append(str(COMPOSE_FILE.parent / INTRON_OVERLAY))
    cmd = ["docker", "compose"] + [a for f in files for a in ("-f", f)] + ["--profile", profile, "config", "--format", "json"]
    out = subprocess.run(cmd, capture_output=True, text=True, env={**os.environ, **BENCH_ENV, **({"NOEMA_BENCH_INTRON_KEY_FILE": "/tmp/noema-intron-key"} if with_intron else {})} if profile == "bench" else None)
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
        if profile == "bench" and name == "model" and "--no-access-log" not in (svc.get("entrypoint") or []):
            # The G1 workflow captures the model's log and scans it (criterion 8). Uvicorn's access log names the client's ephemeral port on every request, and the health check makes one
            # every three seconds: five-digit ports are digit runs, so a clean run would fail its own scan (Codex P1 on noema#591).
            bad.append(f"{profile}: the model's uvicorn must run with --no-access-log (its access log carries the client port, a digit run the leak scan would report)")
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
            # The synthetic IdP's material (benchmark/out/idp): the core holds the PUBLIC key and nothing else of it; the private key and the issuer binary are read by the runner only, so no
            # other service may mount anything of that directory (a session token the core could mint would be an identity the core invents, INV-12).
            idp_src = str(vol.get("source", "")).replace("\\", "/")
            # The output directory itself, or a folder above it, holds the idp folder too (SR on noema#577, note b): the runner's own mount of /bench-out names the directory, and any
            # other service mounting it or a parent of it would get the private key without the path containing "/idp".
            out_roots = [str(v.get("source", "")).replace("\\", "/") for sv in cfg["services"].values() for v in (sv.get("volumes") or []) if v.get("target") == "/bench-out"]
            holds_idp = any(root and (idp_src == root or root.startswith(idp_src.rstrip("/") + "/")) for root in out_roots) and bool(idp_src) and vol.get("type") == "bind"
            if "/idp/" in idp_src or idp_src.endswith("/idp") or holds_idp:
                if not (name == "core" and idp_src.endswith("/idp/key/public.pem")) and name != "bench-runner":
                    bad.append(f"{profile}: {name} mounts {idp_src} from the synthetic IdP's directory; only the core's public key and the runner may (the private key and the issuer stay with the runner)")
            src_norm = str(vol.get("source", "")).replace("\\", "/")
            if "/secrets/tls/" in src_norm and src_norm.rsplit("/", 1)[-1] not in TLS_MOUNTS.get(name, set()):
                bad.append(f"{profile}: {name} mounts {src_norm.rsplit('/', 1)[-1]} from the mTLS folder; it may mount only {sorted(TLS_MOUNTS.get(name, set()))} (the core never holds the model's key, the model never holds the core's)")
            if name == "model":
                target, source = str(vol.get("target", "")), str(vol.get("source", ""))
                # The model gets the benchmark COST directory and nothing else of the output directory: the runner writes the generated suite and its oracle documents beside it
                # (INV-22; Codex P1 on noema#444). So no model mount may be, contain or sit under /bench-out except /bench-out/cost, whose host source is the cost directory itself.
                if target == "/bench-out" or target.startswith("/bench-out/") and target != "/bench-out/cost" or "/bench-out".startswith(target.rstrip("/") + "/") or target == "/":
                    bad.append(f"{profile}: the model mounts {target!r}: only /bench-out/cost may be shared with it (the suite and the oracle live beside it)")
                if target == "/bench-out/cost" and not source.replace("\\", "/").rstrip("/").endswith("/cost"):
                    bad.append(f"{profile}: the model's /bench-out/cost comes from {source!r}, not from a directory named cost")
        credential_names = list((svc.get("environment") or {}).keys()) + [str(s.get("source", s) if isinstance(s, dict) else s) for s in (svc.get("secrets") or [])]
        for cred in sorted(credential_names):
            if PROVIDER_NAME.search(cred) and name != "model":
                bad.append(f"{profile}: {name} holds {cred!r}, a provider credential or endpoint setting: only the model service may (INV-29, AGENT-05)")
            if name in PROVIDER_SIDE and CORE_CREDENTIAL_NAME.search(cred):
                bad.append(f"{profile}: {name} holds {cred!r}, a credential of the core's database, Temporal or grants: the provider side never does (INV-29, PERD 41.1)")
        if name == "core":
            env = svc.get("environment") or {}
            for setting in MODEL_CLIENT_SETTINGS:
                if not str(env.get(setting, "")).strip():
                    bad.append(f"{profile}: the core does not set {setting}: its model client is built only when all five of {list(MODEL_CLIENT_SETTINGS)} are set (noema#563)")
        grant_refs = [str(x if isinstance(x, str) else x.get("source", "")) for x in (svc.get("secrets") or [])] + list((svc.get("environment") or {}).keys()) \
            + [str(v.get("source", "")) + " " + str(v.get("target", "")) for v in (svc.get("volumes") or [])]
        names_grant = any("grant_hmac" in r.lower() or "grant_key" in r.lower() or "grant.hmac" in r.lower() for r in grant_refs)
        if name not in (GRANT_INIT, "core") and names_grant:
            bad.append(f"{profile}: {name} names or mounts a Decision Grant key; only the core (the grant_keys volume) and {GRANT_INIT} may (SEC-09, INV-29)")
        vols = [v for v in (svc.get("volumes") or []) if isinstance(v, dict)]
        for v in vols:  # the secrets folder holds the keys: no service may mount the folder itself, only single named files under it
            src = str(v.get("source", "")).rstrip("/")
            if v.get("type") == "bind" and src.rsplit("/", 1)[-1] == "secrets":
                bad.append(f"{profile}: {name} bind-mounts the secrets folder {src!r}, which holds the grant keys; mount single files only (SEC-09, INV-29)")
        held = {str(x if isinstance(x, str) else x.get("source", "")): (x.get("target") if isinstance(x, dict) else None) for x in (svc.get("secrets") or [])}
        if name == "core":
            for key in GRANT_KEY_SECRETS:
                if key in held:
                    bad.append(f"{profile}: the core holds the grant key secret {key}; Compose cannot give it the mode the loader needs, so only {GRANT_INIT} holds it (SEC-09)")
            gv = [v for v in vols if v.get("source") == GRANT_VOLUME]
            if len(gv) != 1 or not gv[0].get("read_only"):
                bad.append(f"{profile}: the core must mount the {GRANT_VOLUME} volume exactly once, read-only (SEC-09)")
            if (svc.get("depends_on") or {}).get(GRANT_INIT, {}).get("condition") != "service_completed_successfully":
                bad.append(f"{profile}: the core must wait for {GRANT_INIT} to complete successfully, or it starts without keys it can load (SEC-09)")
        if name == GRANT_INIT:
            for key, target in GRANT_KEY_SECRETS.items():
                if held.get(key) != target:
                    bad.append(f"{profile}: {GRANT_INIT} must hold {key} under the name {target} (the loader reads grant.hmac.<version>.key; SEC-09)")
            if set(held) - set(GRANT_KEY_SECRETS):
                bad.append(f"{profile}: {GRANT_INIT} holds a secret other than the two grant keys (SEC-09)")
            writable = [v for v in vols if v.get("source") == GRANT_VOLUME and not v.get("read_only")]
            if len(writable) != 1:
                bad.append(f"{profile}: {GRANT_INIT} must mount {GRANT_VOLUME} writable exactly once (SEC-09)")
            if str(svc.get("user", "")) not in ("0:0", "0", "root"):
                bad.append(f"{profile}: {GRANT_INIT} must run as root to set the key file owner (SEC-09)")
        # A356: the tenant login password and the DSN files it makes.
        if TENANT_LOGIN_SECRET in held and name not in ("postgres", TRUST_DSN_INIT):
            bad.append(f"{profile}: {name} holds {TENANT_LOGIN_SECRET}; only postgres and {TRUST_DSN_INIT} may (INV-29, A356)")
        dsn_mounts = [v for v in vols if v.get("source") == TRUST_DSN_VOLUME]
        if dsn_mounts and name not in ("core", TRUST_DSN_INIT):
            bad.append(f"{profile}: {name} mounts the {TRUST_DSN_VOLUME} volume; only the core (read-only) and {TRUST_DSN_INIT} may (INV-29, A356)")
        if name == TRUST_DSN_INIT:
            if set(held) != {TENANT_LOGIN_SECRET}:
                bad.append(f"{profile}: {TRUST_DSN_INIT} must hold the secret {TENANT_LOGIN_SECRET} and nothing else (A356)")
            if len([v for v in dsn_mounts if not v.get("read_only")]) != 1:
                bad.append(f"{profile}: {TRUST_DSN_INIT} must mount {TRUST_DSN_VOLUME} writable exactly once (A356)")
        if name == "core" and profile == "bench":
            for setting, value in BENCH_CORE_SETTINGS.items():
                got = str((svc.get("environment") or {}).get(setting, ""))
                if got != value:
                    bad.append(f"{profile}: the core's {setting} is {got!r}, the Architect's bench value is {value!r} (A356; AGENT-16: a number changes by ruling)")
            if "NOEMA_TRUST_AT_RISK_BPS" in (svc.get("environment") or {}):
                bad.append(f"{profile}: the core sets NOEMA_TRUST_AT_RISK_BPS; trustlimits reads it from the pack (A356 as corrected)")
            if len(dsn_mounts) != 1 or not dsn_mounts[0].get("read_only"):
                bad.append(f"{profile}: the core must mount the {TRUST_DSN_VOLUME} volume exactly once, read-only (A356)")
            if (svc.get("depends_on") or {}).get(TRUST_DSN_INIT, {}).get("condition") != "service_completed_successfully":
                bad.append(f"{profile}: the core must wait for {TRUST_DSN_INIT} to complete successfully, or it starts without a Trust login (A356)")
        if name == "temporal":
            # Temporal TLS (A296): in the bench profile the frontend presents a certificate and the CLI the healthcheck and the namespace set-up use trusts the stack's CA, so a plain
            # client is refused (EL's "refuse plain" test follows). The server keeps only its own key and the CA certificate.
            if profile == "bench":
                env = svc.get("environment") or {}
                for setting in TEMPORAL_TLS_SETTINGS:
                    if not str(env.get(setting, "")).strip():
                        bad.append(f"{profile}: temporal does not set {setting}: the bench Temporal must serve TLS (A296)")
                mounted_tls = {str(v.get("source", "")).replace("\\", "/").rsplit("/", 1)[-1] for v in svc.get("volumes", []) or [] if "/secrets/tls/" in str(v.get("source", "")).replace("\\", "/")}
                for need in sorted(TLS_MOUNTS["temporal"] - mounted_tls):
                    bad.append(f"{profile}: temporal does not mount {need} from the mTLS folder (A296)")
                if "--tls" not in " ".join(map(str, (svc.get("healthcheck") or {}).get("test", []))):
                    bad.append(f"{profile}: temporal's healthcheck does not use TLS (A296)")
            elif any(str(k).startswith("TEMPORAL_TLS") for k in (svc.get("environment") or {})):
                bad.append(f"{profile}: temporal sets TLS outside the bench profile; A296 puts it in bench only until the core's client follows")
        # Intron's key (A249): a file mount read by the model service only. No service may carry it as an environment variable, and no service but the model may name a setting or a mount for it.
        env_names = set((svc.get("environment") or {}).keys()) if isinstance(svc.get("environment"), dict) else set()
        mounts = [str(v.get("source", "")) + " " + str(v.get("target", "")) for v in (svc.get("volumes") or [])] + [str(x if isinstance(x, str) else x.get("source", "")) for x in (svc.get("secrets") or [])]
        if "INTRON_API_KEY" in env_names:
            bad.append(f"{profile}: {name} carries the Intron key as an environment variable; it comes only from a file mount (A249)")
        if name != "model" and ("INTRON_API_KEY_FILE" in env_names or any("intron" in m.lower() for m in mounts)):
            bad.append(f"{profile}: {name} names or mounts Intron's key; only the model service may (A249, INV-29)")
        if name == "model":
            # A377 to A379: the key file is mounted in the bench profile only, read-only, as one bind of one file at one path, and the variable and the mount come together or not at all.
            key_env = str((svc.get("environment") or {}).get("INTRON_API_KEY_FILE", ""))
            key_mounts = [v for v in (svc.get("volumes") or []) if isinstance(v, dict) and str(v.get("target", "")) == INTRON_TARGET]
            if key_env and profile != "bench":
                bad.append(f"{profile}: the model has INTRON_API_KEY_FILE; the key mount exists in the bench profile only (A377 to A379)")
            if bool(key_env) != bool(key_mounts):
                bad.append(f"{profile}: the model's INTRON_API_KEY_FILE and its key mount must come together: a variable with no file, or a file with no variable (A379)")
            if key_env and key_env != INTRON_TARGET:
                bad.append(f"{profile}: INTRON_API_KEY_FILE is {key_env!r}, not {INTRON_TARGET} (A379)")
            for v in key_mounts:
                if (v.get("bind") or {}).get("create_host_path") is not False:
                    bad.append(f"{profile}: the Intron key bind must set create_host_path: false, or a wrong host path becomes an empty directory at the key path (A379)")
                if v.get("type") != "bind" or not v.get("read_only") or str(v.get("source", "")).rstrip("/").endswith(("secrets", "/")) or len(key_mounts) != 1:
                    bad.append(f"{profile}: the Intron key must be one read-only bind of one file, never a folder (A379)")
        if name == "webhook-relay" and profile != "bench" and str((svc.get("environment") or {}).get("WEBHOOK_RELAY_HOLD_SECONDS", "")).strip():
            bad.append(f"{profile}: the relay holds its answers (WEBHOOK_RELAY_HOLD_SECONDS is set); that is a bench-only fault (A247)")
        if name == "model":
            for field in ("entrypoint", "command"):
                if shell_wrapper(svc.get(field)):
                    bad.append(f"{profile}: the model's {field} runs through a shell ({shell_wrapper(svc.get(field))}); a shell can build a --ssl-* option at run time that no check can see, so uvicorn must be started directly")
            for flag in listener_flags(svc.get("entrypoint")) + listener_flags(svc.get("command")):
                bad.append(f"{profile}: the model starts uvicorn with {flag}, which overrides its UVICORN_SSL_* settings (the client-certificate requirement can be switched off without the service's own check seeing it)")
            reqs = (svc.get("environment") or {}).get("UVICORN_SSL_CERT_REQS")
            if reqs is not None and str(reqs) != "2":
                bad.append(f"{profile}: the model sets UVICORN_SSL_CERT_REQS to {reqs!r}; it must be 2 (a client certificate is required)")
        if name == "core" and profile == "bench" and not str((svc.get("environment") or {}).get("NOEMA_PAYLOAD_MAX_BYTES", "")).strip():
            bad.append(f"{profile}: the core does not set NOEMA_PAYLOAD_MAX_BYTES: the evidence payload reader refuses to be built without its bound (noema#606, A236)")
        if name in TLS_MOUNTS:
            mounted = {str(v.get("source", "")).replace("\\", "/").rsplit("/", 1)[-1] for v in svc.get("volumes", []) or [] if "/secrets/tls/" in str(v.get("source", "")).replace("\\", "/")}
            for need in sorted(TLS_REQUIRED.get(name, set()) - mounted):
                bad.append(f"{profile}: {name} does not mount {need} from the mTLS folder: without it the {'listener cannot start' if name == 'model' else 'core has no client identity'} (noema#555)")
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
    dev, ci, bench, demo = configs["dev"], configs["ci"], configs["bench"], configs["demo"]  # configs["bench+intron"] is the bench stack with the optional key overlay (A377 to A379)
    sidecar = {"network_mode": "none"}
    cases = {  # label -> (profile, mutation applied to a copy of that profile's configuration)
        "core joins bench_net (oracle)": ("dev", lambda c: c["services"]["core"]["networks"].update({"bench_net": None})),
        "model leaves svc_net": ("dev", lambda c: c["services"]["model"]["networks"].pop("svc_net")),
        "unknown service": ("dev", lambda c: c["services"].update({"extra": {"networks": {"edge_net": None}}})),
        "postgres publishes a port": ("dev", lambda c: c["services"]["postgres"].update({"ports": [{"host_ip": "127.0.0.1", "published": "5432"}]})),
        "ui binds all interfaces": ("dev", lambda c: c["services"]["ui"].update({"ports": [{"published": "8082"}]})),
        "the bench model logs every request with the client port": ("bench", lambda c: c["services"]["model"].update({"entrypoint": [a for a in c["services"]["model"]["entrypoint"] if a != "--no-access-log"]})),
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
        "the core holds a provider key (INV-29)": ("dev", lambda c: c["services"]["core"].setdefault("environment", {}).update({"INTRON_API_KEY": "x"})),
        "the core holds a provider endpoint setting": ("ci", lambda c: c["services"]["core"].setdefault("environment", {}).update({"INTRON_API_BASE_URL": "x"})),
        "the ui holds a provider key in bench": ("bench", lambda c: c["services"]["ui"].setdefault("environment", {}).update({"OPENAI_API_KEY": "x"})),
        "the evil provider holds a real provider key": ("bench", lambda c: c["services"]["evil-provider"].setdefault("environment", {}).update({"AETHEX_API_KEY": "x"})),
        "the core mounts a provider key as a Compose secret": ("dev", lambda c: c["services"]["core"].update({"secrets": [{"source": "intron_api_key"}]})),
        "postgres holds a provider setting": ("demo", lambda c: c["services"]["postgres"].setdefault("environment", {}).update({"ANTHROPIC_API_KEY": "x"})),
        "the core holds the Azure Foundry endpoint (Codex P2 on noema#641)": ("dev", lambda c: c["services"]["core"].setdefault("environment", {}).update({"AZURE_FOUNDRY_ENDPOINT": "x"})),
        "the core holds the Azure Foundry key": ("ci", lambda c: c["services"]["core"].setdefault("environment", {}).update({"AZURE_FOUNDRY_API_KEY": "x"})),
        "the ui holds an OpenRouter key": ("demo", lambda c: c["services"]["ui"].setdefault("environment", {}).update({"OPENROUTER_API_KEY": "x"})),
        "postgres holds a Typesafe key": ("dev", lambda c: c["services"]["postgres"].setdefault("environment", {}).update({"TYPESAFE_API_KEY": "x"})),
        "the model holds the core's database password file (Codex P1 on noema#641)": ("dev", lambda c: c["services"]["model"].setdefault("environment", {}).update({"NOEMA_DB_PASSWORD_FILE": "/run/secrets/noema_core_password"})),
        "the model mounts the core's database password as a Compose secret": ("ci", lambda c: c["services"]["model"].update({"secrets": [{"source": "noema_core_password"}]})),
        "the evil provider holds a Postgres password file": ("bench", lambda c: c["services"]["evil-provider"].setdefault("environment", {}).update({"POSTGRES_PASSWORD_FILE": "x"})),
        "the model holds the core's database DSN": ("dev", lambda c: c["services"]["model"].setdefault("environment", {}).update({"NOEMA_CORE_DSN": "x"})),
        "the model holds a Temporal setting": ("ci", lambda c: c["services"]["model"].setdefault("environment", {}).update({"TEMPORAL_ADDRESS": "x"})),
        "the model holds a grant signing key": ("demo", lambda c: c["services"]["model"].setdefault("environment", {}).update({"NOEMA_GRANT_KEY_FILE": "x"})),
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
        "the core mounts the IdP's private key": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/idp/key/private.pem", "target": "/run/idp/private.pem"})),
        "the core mounts the whole bench output directory": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out", "target": "/run/out"})),
        "the stand-in mounts a folder above the bench output": ("bench", lambda c: c["services"]["standin"].setdefault("volumes", []).append({"type": "bind", "source": "/tmp", "target": "/x"})),
        "the core mounts the IdP directory": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/idp", "target": "/run/idp-all"})),
        "the model mounts the issuer binary": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/idp/synthetic-idp", "target": "/usr/local/bin/synthetic-idp"})),
        "the stand-in mounts the IdP public key": ("bench", lambda c: c["services"]["standin"].setdefault("volumes", []).append({"type": "bind", "source": "/tmp/noema-bench-out/idp/key/public.pem", "target": "/x"})),
        "the model mounts the whole bench output directory": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out", "target": "/bench-out"})),
        "the model mounts the generated suite": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/suite", "target": "/bench-out/suite"})),
        "the model mounts the oracle directory": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/tmp/noema-bench-out/suite/oracle", "target": "/bench-out/suite/oracle"})),
        "the model mounts the filesystem root": ("bench", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/", "target": "/"})),
        "the model's cost mount is the whole output directory": ("bench", lambda c: [v.update({"source": "/tmp/noema-bench-out"}) for v in c["services"]["model"]["volumes"] if v.get("target") == "/bench-out/cost"]),
        "the core mounts the model's server key": ("dev", lambda c: c["services"]["core"]["volumes"].append({"type": "bind", "source": "/x/secrets/tls/model.key", "target": "/run/tls/model.key"})),
        "the model mounts the core's client key": ("dev", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/x/secrets/tls/core-client.key", "target": "/run/tls/core-client.key"})),
        "the ui mounts the mTLS CA": ("dev", lambda c: c["services"]["ui"].setdefault("volumes", []).append({"type": "bind", "source": "/x/secrets/tls/ca.crt", "target": "/run/tls/ca.crt"})),
        "the model loses its server certificate mount": ("dev", lambda c: c["services"]["model"].update({"volumes": [v for v in c["services"]["model"]["volumes"] if not str(v.get("source", "")).endswith("model.crt")]})),
        "the core loses its client key mount": ("ci", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if not str(v.get("source", "")).endswith("core-client.key")]})),
        "the model carries the Intron key as a variable": ("dev", lambda c: c["services"]["model"]["environment"].update({"INTRON_API_KEY": "x"})),
        "the core is given the Intron key file": ("dev", lambda c: c["services"]["core"]["environment"].update({"INTRON_API_KEY_FILE": "/run/secrets/intron_api_key"})),
        "the UI mounts the Intron key file": ("dev", lambda c: c["services"]["ui"].setdefault("volumes", []).append({"type": "bind", "source": "/h/secrets/intron_api_key", "target": "/run/intron_api_key"})),
        "the bench model gets the key file variable but no mount": ("bench", lambda c: c["services"]["model"]["environment"].update({"INTRON_API_KEY_FILE": "/run/intron/key"})),
        "the bench model with the key mount loses the variable": ("bench+intron", lambda c: c["services"]["model"]["environment"].pop("INTRON_API_KEY_FILE")),
        "the bench key mount is writable": ("bench+intron", lambda c: [v.update({"read_only": False}) for v in c["services"]["model"]["volumes"] if v.get("target") == "/run/intron/key"]),
        "the bench key mount may create its host path": ("bench+intron", lambda c: [v.pop("bind", None) for v in c["services"]["model"]["volumes"] if v.get("target") == "/run/intron/key"]),
        "the bench key mount is a folder named secrets": ("bench+intron", lambda c: [v.update({"source": "/h/secrets"}) for v in c["services"]["model"]["volumes"] if v.get("target") == "/run/intron/key"]),
        "the bench key mount is a named volume": ("bench+intron", lambda c: [v.update({"type": "volume"}) for v in c["services"]["model"]["volumes"] if v.get("target") == "/run/intron/key"]),
        "the key file variable points elsewhere": ("bench+intron", lambda c: c["services"]["model"]["environment"].update({"INTRON_API_KEY_FILE": "/run/other"})),
        "the core mounts the key file too": ("bench+intron", lambda c: c["services"]["core"].setdefault("volumes", []).append({"type": "bind", "source": "/tmp/noema-intron-key", "target": "/run/intron/key", "read_only": True})),
        "the dev model has the key file variable": ("dev", lambda c: [c["services"]["model"]["environment"].update({"INTRON_API_KEY_FILE": "/run/intron/key"}), c["services"]["model"].setdefault("volumes", []).append({"type": "bind", "source": "/h/k", "target": "/run/intron/key", "read_only": True})]),
        "the ci relay holds its answers": ("ci", lambda c: c["services"]["webhook-relay"]["environment"].update({"WEBHOOK_RELAY_HOLD_SECONDS": "15"})),
        "the ui lists a grant key as a secret": ("dev", lambda c: c["services"]["ui"].update({"secrets": [{"source": "grant_hmac_key_a"}]})),
        "the model carries a grant key file setting": ("ci", lambda c: c["services"]["model"].setdefault("environment", {}).update({"NOEMA_GRANT_KEY_FILE": "/x"})),
        "postgres mounts a grant key file": ("demo", lambda c: c["services"]["postgres"].setdefault("volumes", []).append({"type": "bind", "source": "/h/secrets/grant_hmac_key_b", "target": "/x"})),
        "the core is given a grant key secret": ("dev", lambda c: c["services"]["core"].update({"secrets": ["noema_core_password", "grant_hmac_key_a"]})),
        "the core drops the grant_keys volume": ("dev", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if not (isinstance(v, dict) and v.get("source") == "grant_keys")]})),
        "the core mounts grant_keys writable": ("ci", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if isinstance(v, dict) and v.get("source") == "grant_keys"]),
        "the core does not wait for the key step": ("dev", lambda c: c["services"]["core"]["depends_on"].pop("grant-keys-init")),
        "the key step holds one key under the wrong name": ("dev", lambda c: c["services"]["grant-keys-init"].update({"secrets": [{"source": "grant_hmac_key_a", "target": "key.a"}, {"source": "grant_hmac_key_b", "target": "grant.hmac.2.key"}]})),
        "the key step also holds the core password": ("dev", lambda c: c["services"]["grant-keys-init"]["secrets"].append({"source": "noema_core_password", "target": "noema_core_password"})),
        "the key step runs as the core's user": ("demo", lambda c: c["services"]["grant-keys-init"].update({"user": "65532:65532"})),
        "the key step is dropped": ("ci", lambda c: c["services"].pop("grant-keys-init")),
        "the model mounts the grant_keys volume": ("dev", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "grant_keys", "target": "/k"})),
        "a service bind-mounts the secrets folder": ("dev", lambda c: c["services"]["ui"].setdefault("volumes", []).append({"type": "bind", "source": "/h/deploy/compose/secrets", "target": "/s"})),
        "the core bind-mounts the secrets folder": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "bind", "source": "/h/deploy/compose/secrets/", "target": "/s", "read_only": True})),
        "the bench model starts uvicorn with --ssl-cert-reqs 0": ("bench", lambda c: c["services"]["model"].update({"entrypoint": list(c["services"]["model"]["entrypoint"]) + ["--ssl-cert-reqs", "0"]})),
        "the dev model's command carries an --ssl-keyfile": ("dev", lambda c: c["services"]["model"].update({"command": ["--ssl-keyfile", "/x"]})),
        "the ci model's command is one string with --ssl-cert-reqs=0": ("ci", lambda c: c["services"]["model"].update({"command": "--port 8081 --ssl-cert-reqs=0"})),
        "the dev model's command is a shell wrapper carrying --ssl-cert-reqs 0 (list form)": ("dev", lambda c: c["services"]["model"].update({"command": ["sh", "-c", "uvicorn noema_model.main:app --ssl-cert-reqs 0"]})),
        "the ci model's entrypoint is a shell wrapper with no visible --ssl option": ("ci", lambda c: c["services"]["model"].update({"entrypoint": ["sh", "-c", "exec python -m uvicorn noema_model.main:app --port 8081"]})),
        "the dev model's entrypoint is bash -lc": ("dev", lambda c: c["services"]["model"].update({"entrypoint": ["/bin/bash", "-lc", "uvicorn noema_model.main:app"]})),
        "the dev model's command is a shell wrapper in string form": ("dev", lambda c: c["services"]["model"].update({"command": "sh -c 'uvicorn noema_model.main:app --ssl-cert-reqs=0'"})),
        "the dev model's list element holds a quoted --ssl-keyfile": ("dev", lambda c: c["services"]["model"].update({"command": ["uvicorn noema_model.main:app '--ssl-keyfile' /x"]})),
        "the model's UVICORN_SSL_CERT_REQS is 0": ("dev", lambda c: c["services"]["model"].setdefault("environment", {}).update({"UVICORN_SSL_CERT_REQS": "0"})),
        "the bench temporal drops its frontend certificate": ("bench", lambda c: c["services"]["temporal"]["environment"].pop("TEMPORAL_TLS_FRONTEND_CERT")),
        "the bench temporal drops its internode server certificate": ("bench", lambda c: c["services"]["temporal"]["environment"].pop("TEMPORAL_TLS_SERVER_CERT")),
        "the bench temporal drops its internode server key": ("bench", lambda c: c["services"]["temporal"]["environment"].pop("TEMPORAL_TLS_SERVER_KEY")),
        "the bench temporal drops the internode server name": ("bench", lambda c: c["services"]["temporal"]["environment"].pop("TEMPORAL_TLS_INTERNODE_SERVER_NAME")),
        "the bench temporal drops its CA setting": ("bench", lambda c: c["services"]["temporal"]["environment"].pop("TEMPORAL_TLS_SERVER_CA_CERT")),
        "the bench temporal healthcheck goes back to plain": ("bench", lambda c: c["services"]["temporal"]["healthcheck"].update({"test": ["CMD-SHELL", "temporal operator cluster health --address x:7233"]})),
        "the bench temporal stops mounting its key": ("bench", lambda c: c["services"]["temporal"].update({"volumes": [v for v in c["services"]["temporal"]["volumes"] if not str(v.get("source", "")).endswith("temporal.key")]})),
        "temporal serves TLS in dev (outside the bench profile)": ("dev", lambda c: c["services"]["temporal"].setdefault("environment", {}).update({"TEMPORAL_TLS_FRONTEND_CERT": "/x"})),
        "the model mounts the temporal key": ("dev", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "bind", "source": "/h/secrets/tls/temporal.key", "target": "/x"})),
        "the bench core drops the trust_dsns volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "trust_dsns"]})),
        "the bench core mounts trust_dsns writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if v.get("source") == "trust_dsns"]),
        "the bench core does not wait for trust-dsn-init": ("bench", lambda c: c["services"]["core"]["depends_on"].pop("trust-dsn-init")),
        "the bench core holds the tenant login password": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "noema_tenant_login_password", "target": "x"})),
        "the bench model mounts trust_dsns": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "trust_dsns", "target": "/x", "read_only": True})),
        "trust-dsn-init also holds the core password": ("bench", lambda c: c["services"]["trust-dsn-init"]["secrets"].append({"source": "noema_core_password", "target": "x"})),
        "trust-dsn-init is dropped": ("bench", lambda c: c["services"].pop("trust-dsn-init")),
        "the bench core's store timeout changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_STORE_TIMEOUT_MS": "5000"})),
        "the bench core loses a settings-table value": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_DB_SERIALIZATION_ATTEMPTS")),
        "the bench core sets the at-risk limit itself": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TRUST_AT_RISK_BPS": "2500"})),
        "the bench core loses the evidence payload bound": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_PAYLOAD_MAX_BYTES")),
        "the core loses the model response bound": ("dev", lambda c: c["services"]["core"]["environment"].pop("NOEMA_MODEL_MAX_RESPONSE_BYTES")),
        "the core loses the model client certificate setting": ("ci", lambda c: c["services"]["core"]["environment"].pop("NOEMA_MODEL_TLS_CERT_FILE")),
        "the core's model URL is empty": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_MODEL_URL": ""})),
        "the stand-in leaves bench_net": ("ci", lambda c: c["services"]["standin"]["networks"].pop("bench_net")),
    }
    missed = []
    for label, (profile, mutate) in cases.items():
        mutated = copy.deepcopy(configs[profile])
        mutate(mutated)
        if not check(mutated, profile.split("+")[0]):
            missed.append(label)
    # And the unmutated configurations must be clean, so a firing rule is the mutation and not noise.
    for profile, cfg in configs.items():
        if check(cfg, profile.split("+")[0]):
            missed.append(f"unmutated {profile} is not clean")
    # The inventory: every name of .env.example that is a key or an endpoint must be classified as a provider name, or this rule would not know it (Codex P2 on noema#641).
    env_example = COMPOSE_FILE.parent.parent.parent / ".env.example"
    if env_example.is_file():
        for line in env_example.read_text(encoding="utf-8").splitlines():
            name = line.split("=", 1)[0].strip()
            if "=" in line and re.fullmatch(r"[A-Z][A-Z0-9_]*", name) and re.search(r"(API_KEY|_ENDPOINT)$", name) and not PROVIDER_NAME.search(name):
                missed.append(f".env.example names {name}, which the provider-credential rule does not classify as a provider name: add its prefix to PROVIDER_NAME")
    else:
        missed.append(".env.example is missing: the provider inventory cannot be checked")
    # Positive control for the credential rule: the model service IS allowed its provider key, endpoint and secret (a rule that refused it would make the allowed place unusable).
    allowed = copy.deepcopy(configs["dev"])
    allowed["services"]["model"].setdefault("environment", {}).update({"INTRON_API_KEY": "x", "INTRON_API_BASE_URL": "x", "OPENAI_API_KEY": "x"})
    allowed["services"]["model"]["secrets"] = [{"source": "intron_api_key"}]
    if any("credential" in item for item in check(allowed, "dev")):
        missed.append("the model service is refused its own provider credentials")
    return missed, len(cases)


def main():
    failures = []
    configs = {}
    for profile in PROFILES:
        cfg = configs[profile] = resolved(profile)
        found = check(cfg, profile)
        print(f"{'FAIL' if found else 'ok  '} profile {profile:6} services: {', '.join(sorted(cfg['services']))}")
        failures += found
    # The bench stack with the optional Intron key overlay (A377 to A379): the same rules, and the key mount is the model's alone.
    configs["bench+intron"] = resolved("bench", with_intron=True)
    found = check(configs["bench+intron"], "bench")
    print(f"{'FAIL' if found else 'ok  '} profile bench with the optional Intron key overlay")
    failures += found
    # The names the core reads are the Env... constants of its model client (noema#563). When that file is in this tree the two lists must be the same set, so the Compose file and the
    # code cannot drift apart again; on a tree without it (before noema#563 merges) only the Compose side is checked, and this line says so.
    if MODEL_CLIENT_SOURCE.is_file():
        import re
        names = set(re.findall(r'Env\w+\s*=\s*"(NOEMA_MODEL_[A-Z_]+)"', MODEL_CLIENT_SOURCE.read_text(encoding="utf-8")))
        if names != set(MODEL_CLIENT_SETTINGS):
            failures.append(f"drift: services/core/cmd/core/modelclient.go reads {sorted(names)} but profiles.py expects {sorted(MODEL_CLIENT_SETTINGS)}")
        else:
            print(f"ok   the core's model client reads exactly the {len(names)} settings Compose gives it")
    else:
        print("note the core's model client (services/core/cmd/core/modelclient.go) is not in this tree: only the Compose side of its five settings is checked")
    if MODEL_DOCKERFILE.is_file():
        text = MODEL_DOCKERFILE.read_text(encoding="utf-8")
        found = dockerfile_problems(text)
        failures += found
        if not found:
            print("ok   the model image requires a client certificate and starts uvicorn with no --ssl-* option of its own")
        if "--self-test" in sys.argv:
            mutants = {
                "the image sets UVICORN_SSL_CERT_REQS=0": text.replace("UVICORN_SSL_CERT_REQS=2", "UVICORN_SSL_CERT_REQS=0"),
                "the image drops UVICORN_SSL_CERT_REQS": text.replace("UVICORN_SSL_CERT_REQS=2", ""),
                "the entrypoint carries --ssl-cert-reqs": text.replace('"--port", "8081"]', '"--port", "8081", "--ssl-cert-reqs", "0"]'),
                "the --ssl option is on a continuation line of the ENTRYPOINT": text.replace('"--port", "8081"]', '"--port", "8081", ' + chr(92) + chr(10) + '    "--ssl-cert-reqs", "0"]'),
                "the --ssl option is on a continuation line of a CMD": text + chr(10) + "CMD [" + chr(34) + "--port" + chr(34) + ", " + chr(92) + chr(10) + "# a comment inside the instruction" + chr(10) + "  " + chr(34) + "--ssl-keyfile=/x" + chr(34) + "]" + chr(10),
                "a later ENV sets UVICORN_SSL_CERT_REQS back to 0": text + chr(10) + "ENV UVICORN_SSL_CERT_REQS=0" + chr(10),
                "the image changes its escape character": "# escape=`" + chr(10) + text,
                "a later legacy ENV (key, space, value) sets UVICORN_SSL_CERT_REQS back to 0": text + chr(10) + "ENV UVICORN_SSL_CERT_REQS 0" + chr(10),
                "a later ENV with several pairs sets it back to 0": text + chr(10) + "ENV A=1 UVICORN_SSL_CERT_REQS=0 B=2" + chr(10),
            }
            for label, mutant in mutants.items():
                if mutant == text or not dockerfile_problems(mutant):
                    failures.append(f"self-test: the Dockerfile rule did not fire for: {label}")
            print(f"self-test: {len(mutants)} Dockerfile mutations;", "every one is caught" if not any(f.startswith("self-test: the Dockerfile") for f in failures) else "NOT CAUGHT")
    else:
        failures.append(f"{MODEL_DOCKERFILE} is missing: the model image's listener settings cannot be checked")
    if "--self-test" in sys.argv:
        missed, n = self_test(configs)
        print(f"self-test: {n} mutations;", "every one is caught" if not missed else "NOT CAUGHT: " + "; ".join(missed))
        failures += [f"self-test: rule did not fire for: {m}" for m in missed]
    for f in failures:
        print("  -", f)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
