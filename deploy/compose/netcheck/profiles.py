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
    "core-tls-init": set(),
    "trust-dsn-init": set(),
    "core-keys-init": set(),
    "operator-dsn-init": set(),
    "core-trust-keys-init": set(),
    "migrate": {"data_net"},
    "tenants-create": {"data_net"},
    "tenants-provision": {"data_net"},
    "tenant-init": {"data_net"},
    "policy-publish": {"data_net"},
    "webhook-relay-credentials": set(),
}
SIDECARS = {"core-tls-init", "mock-case-credentials", "hostility-proxy-credentials", "webhook-relay-credentials", "standin-credentials", "grant-keys-init", "trust-dsn-init", "core-keys-init", "operator-dsn-init", "core-trust-keys-init"}
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
    "dev": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "core-tls-init"},
    "ci": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "core-tls-init", "mock-case", "mock-case-db", "mock-case-credentials", "hostility-proxy",
           "hostility-proxy-anchor", "hostility-proxy-credentials", "webhook-relay", "webhook-relay-credentials", "standin", "standin-anchor", "standin-credentials"},
    "bench": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "core-tls-init", "mock-case", "mock-case-db", "mock-case-credentials", "hostility-proxy",
              "hostility-proxy-anchor", "hostility-proxy-credentials", "evil-provider", "standin", "standin-anchor", "standin-credentials", "bench-runner", "trust-dsn-init", "core-keys-init", "operator-dsn-init", "core-trust-keys-init", "migrate", "tenants-create", "tenants-provision", "tenant-init", "policy-publish"},
    "demo": {"core", "model", "postgres", "temporal", "ui", "grant-keys-init", "core-tls-init", "mock-case", "mock-case-db", "mock-case-credentials"},
}
ONLY_IN = {"evil-provider": {"bench"}, "hostility-proxy": {"ci", "bench"}, "hostility-proxy-anchor": {"ci", "bench"},
           "hostility-proxy-credentials": {"ci", "bench"}, "webhook-relay": {"ci"}, "webhook-relay-credentials": {"ci"},
           "standin": {"ci", "bench"}, "standin-anchor": {"ci", "bench"}, "standin-credentials": {"ci", "bench"}, "bench-runner": {"bench"},
           "operator-dsn-init": {"bench"}, "core-trust-keys-init": {"bench"}, "migrate": {"bench"}, "tenants-create": {"bench"}, "tenants-provision": {"bench"}, "tenant-init": {"bench"}, "policy-publish": {"bench"}}


# The bench stack is started with these overlays (make bench-up). Their placeholder variables are set here only so `config` can resolve them.
# The mTLS material between the core and the model service (noema#555), from deploy/compose/tls_local.sh: each side mounts its own key and the CA certificate, never the other's key.
TLS_MOUNTS = {"core": {"ca.crt", "core-client.crt"}, "core-tls-init": {"core-client.key"}, "model": {"ca.crt", "model.crt", "model.key", "model-health.crt", "model-health.key"},
              "temporal": {"ca.crt", "temporal.crt", "temporal.key"}}
TLS_REQUIRED = {"core": {"ca.crt", "core-client.crt"}, "model": {"ca.crt", "model.crt", "model.key", "model-health.crt", "model-health.key"}}
# The settings of the core's client of the model service (noema#555, noema#563): the core reads exactly these five and refuses to build its client unless all five are set (no default,
# AGENT-16), so the Compose file must give the core every one. The names are the `Env...` constants of services/core/cmd/core/modelclient.go; main() compares the two when that file exists.
MODEL_CLIENT_SETTINGS = ("NOEMA_MODEL_URL", "NOEMA_MODEL_TLS_CA_FILE", "NOEMA_MODEL_TLS_CERT_FILE", "NOEMA_MODEL_TLS_KEY_FILE", "NOEMA_MODEL_MAX_RESPONSE_BYTES")
MODEL_CLIENT_SOURCE = Path(__file__).resolve().parents[3] / "services" / "core" / "cmd" / "core" / "modelclient.go"
# The Decision Grant HMAC keys (SEC-09; INV-29: the model holds no grant key). Two key files so a rotation has two active keys. Compose cannot set a file secret's mode or owner and the
# grant-key loader refuses a file that is not 0400 and process-owned, so the two secrets are held by the one-shot `grant-keys-init` step ONLY, under the names the loader reads; it writes
# them into the `grant_keys` volume, which the CORE mounts read-only and nothing else mounts (Codex and SR on noema#700). Names only.
GRANT_KEY_SECRETS = {"grant_hmac_key_a": "grant.hmac.1.key", "grant_hmac_key_b": "grant.hmac.2.key"}
GRANT_INIT, GRANT_VOLUME = "grant-keys-init", "grant_keys"
# A667: the core refuses its model-client private key unless the file is a regular file it owns at mode 0400, and Compose can only bind the host file (mode 0600, the host user). The one-shot
# `core-tls-init` is the ONLY service that mounts the host file `secrets/tls/core-client.key`; it copies it into the `core_client_key` volume (owner 65532, mode 0400), which the CORE mounts
# read-only at /run/tls-key and nothing else mounts. The core's reader is not changed. Names and paths only.
TLS_INIT, TLS_KEY_VOLUME, TLS_KEY_TARGET = "core-tls-init", "core_client_key", "/run/tls-key"
TLS_KEY_SETTING = ("NOEMA_MODEL_TLS_KEY_FILE", "/run/tls-key/core-client.key")
TLS_INIT_CAPS = {"CHOWN", "FOWNER", "DAC_READ_SEARCH", "SETUID", "SETGID"}
# A356 (interim, relayed): the bench core's settings with no default in code, the Architect's values, and the per-tenant Trust login DSN files. The tenant login password is a file secret held by
# postgres (init.sh makes the logins) and by the one-shot trust-dsn-init step ONLY; that step writes the DSN files into the trust_dsns volume, which the core mounts read-only and nothing else
# mounts. NOEMA_TRUST_AT_RISK_BPS is not here: trustlimits reads it from the pack. Changing a number is a ruling, not an edit (AGENT-16). Names and numbers only.
BENCH_CORE_SETTINGS = {
    "NOEMA_STORE_TIMEOUT_MS": "1000", "NOEMA_SEAL_TIMEOUT_MS": "1000", "NOEMA_COMPILE_TIMEOUT_MS": "3000", "NOEMA_READ_TIMEOUT_MS": "2000", "NOEMA_TRUST_DB_TIMEOUT_MS": "1000",
    "NOEMA_TRUST_RECONCILE_TIMEOUT_MS": "1000", "NOEMA_GRANT_KEY_MAX_ACTIVE": "2", "NOEMA_DB_SERIALIZATION_ATTEMPTS": "3", "NOEMA_OUTBOX_INTERVAL_MS": "1000",
    "NOEMA_GRANT_KEY_DIR": "/run/grant-keys", "NOEMA_PACK_DIR": "/run/packs", "NOEMA_TRUST_DSN_DIR": "/run/trust-dsn",
    # A445 (interim): the pack bounds are the Architect's bench values (the largest pack file is 64,069 bytes); the manifest path is a setting, not a constant, because a fixed path would name a vertical in core.
    "NOEMA_PACK_MANIFEST": "fintech/manifest.v1.json", "NOEMA_PACK_MAX_BYTES": "1048576", "NOEMA_PACK_MAX_DEPTH": "32",
    # A506 (interim, from the code at the heads of noema#838, main and noema#847): the DSN, key and hash-key paths, the Temporal values, the escalation graph and the Recorder's lifetimes (EL's values in
    # noema#847's body, the Founder to confirm).
    "NOEMA_RECORD_READ_DSN_DIR": "/run/record-read-dsn",
    "NOEMA_ORCHESTRATOR_DSN_DIR": "/run/orchestrator-dsn",
    "NOEMA_EDGE_DSN_DIR": "/run/edge-dsn",
    "NOEMA_ORCHESTRATOR_KEY_DIR": "/run/orchestrator-keys",
    "NOEMA_EDGE_KEY_DIR": "/run/edge-keys",
    "NOEMA_INPUT_HASH_KEY_FILE": "/run/input-hash/key",
    "NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE": "2",
    # A554 and A564 (lane 5): the Execution side's two logins and the receipt and verdict signers' key directories. The overlaps mirror A506's (no ruling gives a number; the Architect confirms).
    "NOEMA_EXECUTION_DSN_DIR": "/run/execution-dsn",
    "NOEMA_OUTCOME_DSN_DIR": "/run/outcome-dsn",
    "NOEMA_EXECUTION_KEY_DIR": "/run/execution-keys",
    "NOEMA_EXECUTION_KEY_MAX_ACTIVE": "2",
    "NOEMA_OUTCOME_KEY_DIR": "/run/outcome-keys",
    "NOEMA_OUTCOME_KEY_MAX_ACTIVE": "2",
    # A722 and A704: the Evidence Layer's login directory, key file and overlap, and the five injection-screen limits (bytes for the pattern length; A722 supersedes "characters").
    "NOEMA_EVIDENCE_DSN_DIR": "/run/evidence-dsn", "NOEMA_EVIDENCE_KEY_FILE": "/run/evidence-keys/core-evidence.ed25519.1.key", "NOEMA_EVIDENCE_KEY_MAX_ACTIVE": "2", "NOEMA_EVIDENCE_RECORD_TIMEOUT_MS": "5000",
    "NOEMA_INJECTION_MAX_PATTERNS": "64", "NOEMA_INJECTION_MAX_PATTERN_BYTES": "1024", "NOEMA_INJECTION_MAX_DOCUMENT_BYTES": "65536", "NOEMA_INJECTION_MAX_DEPTH": "8", "NOEMA_INJECTION_FIELD_CAP": "256",
    # A714: A658's eight events settings, the Execution timeouts and the plan text bound (resource limits, bench only, no default in code).
    "NOEMA_EVENTS_POLL_MS": "2000", "NOEMA_EVENTS_MAX_STREAM_MS": "300000", "NOEMA_EVENTS_WRITE_TIMEOUT_MS": "5000", "NOEMA_EVENTS_READ_TIMEOUT_MS": "2000",
    "NOEMA_EVENTS_MAX_EVENT_BYTES": "65536", "NOEMA_EVENTS_MAX_STREAMS": "100", "NOEMA_EVENTS_MAX_STREAMS_PER_PRINCIPAL": "3", "NOEMA_EVENTS_MAX_BATCH": "100",
    "NOEMA_EXECUTION_EXECUTE_TIMEOUT_MS": "10000", "NOEMA_EXECUTION_ROLLBACK_TIMEOUT_MS": "5000", "NOEMA_PLAN_TEXT_MAX_BYTES": "256",
    "NOEMA_TEMPORAL_NAMESPACE": "default",
    "NOEMA_TEMPORAL_TASK_QUEUE": "noema-investigations",
    "NOEMA_TEMPORAL_TLS_CA_FILE": "/run/tls/ca.crt",
    "NOEMA_TRUST_ESCALATION_GRAPH": "fin.graph.escalate",
    "NOEMA_TRUST_GRANT_LIFETIME_MS": "600000",
    "NOEMA_TRUST_RECORD_LIFETIME_MS": "600000",
    "NOEMA_TRUST_TX_TIMEOUT_MS": "10000",
    # A477 (interim): the gateway, worker and recovery settings; Compose only, bench only, no code default. Durations are Go durations ("5m"), the others whole numbers.
    "NOEMA_INPUT_DEDUP_WINDOW": "5m", "NOEMA_INPUT_RECORD_TIMEOUT": "5s", "NOEMA_INVESTIGATION_QUEUE": "64", "NOEMA_INVESTIGATION_WORKERS": "1", "NOEMA_INVESTIGATION_RUN_TIMEOUT_MS": "180000", "NOEMA_INVESTIGATION_START_TIMEOUT_MS": "30000", "NOEMA_EDGE_KEY_MAX_ACTIVE": "2", "NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS": "240000", "NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS": "30000", "NOEMA_STM_RECOVERY_IDLE_MS": "240000", "NOEMA_STM_RECOVERY_BATCH_SIZE": "50"
}


def oneshot_problems(profile, name, svc, held, vols):
    """ENV-04: the pins of the operator DSN step, the core-trust key step, tenant-init and policy-publish, and who holds the superuser password, the core-trust seed and their volumes."""
    bad = []
    env = svc.get("environment") or {}
    env_names = set(env.keys()) if isinstance(env, dict) else set()
    argv = argv_of(svc)
    # who holds what
    if SUPERUSER_SECRET in held and name not in ("postgres", OPERATOR_INIT):
        bad.append(f"{profile}: {name} holds {SUPERUSER_SECRET}; only postgres and {OPERATOR_INIT} may (INV-29, A527)")
    if MIGRATOR_SECRET in held and name != "postgres":
        bad.append(f"{profile}: {name} holds {MIGRATOR_SECRET}; only postgres does (it reads the mode-644 original; INV-29, A536)")
    if MIGRATOR_FILE_SECRET in held and name not in (MIGRATE, TENANTS_PROVISION):
        bad.append(f"{profile}: {name} holds {MIGRATOR_FILE_SECRET}; only {MIGRATE} and {TENANTS_PROVISION} do (INV-29, A536)")
    if name in (MIGRATE, TENANTS_PROVISION) and set(held) != {MIGRATOR_FILE_SECRET}:
        bad.append(f"{profile}: {name} must hold {MIGRATOR_FILE_SECRET} and nothing else (A536)")
    if name not in ("postgres", MIGRATE, TENANTS_PROVISION) and any("migrator_password" in str(x).lower() or "migrator-password" in str(x).lower() for x in list(env_names) + argv + [str(v.get("source", "")) for v in vols]):
        bad.append(f"{profile}: {name} names the migration login's password in its environment, arguments or mounts (A536)")
    if name == MIGRATE and env_names:
        bad.append(f"{profile}: {MIGRATE} sets environment variables {sorted(env_names)}; the password comes only from the file its flag names, and nothing else needs an environment (A527)")
    if TRUST_KEY_SECRET in held and name != TRUST_KEYS_INIT:
        bad.append(f"{profile}: {name} holds {TRUST_KEY_SECRET}; only {TRUST_KEYS_INIT} may (INV-29, A445)")
    if name not in ("postgres", OPERATOR_INIT) and any("postgres_superuser" in str(x).lower() for x in list(env_names) + argv + [str(v.get("source", "")) for v in vols]):
        bad.append(f"{profile}: {name} names the superuser's password in its environment or arguments (A527)")
    if name in (TENANT_INIT, POLICY_PUBLISH, TENANTS_CREATE) and held:
        bad.append(f"{profile}: {name} holds a secret; it reads files only (A527, INV-29)")
    if name in (TENANT_INIT, POLICY_PUBLISH, MIGRATE, TENANTS_CREATE, TENANTS_PROVISION) and any(re.search(r"(?i)(password|passwd|dsn|key)", k) and not k.endswith("_FILE") for k in env_names):
        bad.append(f"{profile}: {name} carries a credential variable; a password is read from a file (A527, A536, INV-29)")
    for volume, writer, readers in ((OPERATOR_VOLUME, OPERATOR_INIT, (TENANT_INIT, TENANTS_CREATE)), (TRUST_KEYS_VOLUME, TRUST_KEYS_INIT, (TENANT_INIT, POLICY_PUBLISH))):
        mounts = [v for v in vols if v.get("source") == volume]
        if mounts and name not in (writer,) + readers:
            bad.append(f"{profile}: {name} mounts the {volume} volume; only {writer} (writes) and {', '.join(readers)} (read-only) may (INV-29, A527)")
        if name == writer and len([m for m in mounts if not m.get("read_only")]) != 1:
            bad.append(f"{profile}: {writer} must mount {volume} writable exactly once (A527)")
        if name in readers and (len(mounts) != 1 or not mounts[0].get("read_only")):
            bad.append(f"{profile}: {name} must mount the {volume} volume exactly once, read-only (A527)")
    # the two init steps
    if name == OPERATOR_INIT:
        if [str(c).upper() for c in (svc.get("cap_drop") or [])] != ["ALL"] or sorted(str(c).upper() for c in (svc.get("cap_add") or [])) != ["CHOWN", "FOWNER", "SETGID", "SETUID"]:
            bad.append(f"{profile}: {OPERATOR_INIT} must drop ALL capabilities and add back only CHOWN, FOWNER, SETUID and SETGID (A527)")
        if set(held) != {SUPERUSER_SECRET}:
            bad.append(f"{profile}: {OPERATOR_INIT} must hold the secret {SUPERUSER_SECRET} and nothing else (A527)")
        if svc.get("network_mode") != "none":
            bad.append(f"{profile}: {OPERATOR_INIT} must have no network (A527)")
    if name == TRUST_KEYS_INIT:
        if [str(c).upper() for c in (svc.get("cap_drop") or [])] != ["ALL"] or sorted(str(c).upper() for c in (svc.get("cap_add") or [])) != ["CHOWN", "DAC_READ_SEARCH", "FOWNER", "SETGID", "SETUID"]:
            bad.append(f"{profile}: {TRUST_KEYS_INIT} must drop ALL capabilities and add back only CHOWN, DAC_READ_SEARCH, FOWNER, SETUID and SETGID; never DAC_OVERRIDE (A445)")
        if set(held) != {TRUST_KEY_SECRET}:
            bad.append(f"{profile}: {TRUST_KEYS_INIT} must hold the secret {TRUST_KEY_SECRET} and nothing else (A445)")
    # the two one-shots
    if name in ONESHOT_ENTRYPOINTS:
        if svc.get("entrypoint") != [ONESHOT_ENTRYPOINTS[name]]:
            bad.append(f"{profile}: {name} must run {ONESHOT_ENTRYPOINTS[name]} as its entrypoint")
        want_user = bench_user() if name == MIGRATE else CORE_UID_USER
        if str(svc.get("user", "")) != want_user:
            bad.append(f"{profile}: {name} must run as {want_user}{', the host user that owns its password file' if name == MIGRATE else ', the core image' + chr(39) + 's nonroot user'}, never root (A527, A536)")
        if name == MIGRATE and not any(str(t).startswith("/tmp") for t in (svc.get("tmpfs") or [])):
            bad.append(f"{profile}: {MIGRATE} must have a tmpfs at /tmp (A536)")
        if set((svc.get("networks") or {}).keys() if isinstance(svc.get("networks"), dict) else svc.get("networks") or []) != {"data_net"}:
            bad.append(f"{profile}: {name} must be on data_net only (A527)")
        if [str(c).upper() for c in (svc.get("cap_drop") or [])] != ["ALL"] or svc.get("cap_add"):
            bad.append(f"{profile}: {name} must drop ALL capabilities and add none (A527)")
        if not svc.get("read_only"):
            bad.append(f"{profile}: {name} must have a read-only root filesystem (A527)")
        flags = flags_of(argv)
        if any(f is None or v is None for f, v in flags) or len({f for f, _ in flags}) != len(flags):
            bad.append(f"{profile}: {name}'s arguments must be flags with one value each, none repeated (A527)")
        given = dict(flags)
        if name == MIGRATE:
            if given != MIGRATE_FLAGS:
                bad.append(f"{profile}: {MIGRATE} must take exactly {MIGRATE_FLAGS}: no password, DSN or DSN file in an argument (A521, A527)")
            if len([v for v in vols if v.get("type") == "bind" and v.get("target") == "/migrations" and v.get("read_only")]) != 1:
                bad.append(f"{profile}: {MIGRATE} must mount the migrations folder read-only exactly once at /migrations (A484)")
        elif name == TENANT_INIT:
            if given != TENANT_INIT_FLAGS or "--tenant" in given:
                bad.append(f"{profile}: {TENANT_INIT} must take exactly {TENANT_INIT_FLAGS} and no --tenant: tenant rows have one creator (A489)")
        else:
            if set(given) != set(POLICY_PUBLISH_FLAGS) or "--published-by" in given:
                bad.append(f"{profile}: {POLICY_PUBLISH} must take exactly {list(POLICY_PUBLISH_FLAGS)}; the publisher is the database role (A474)")
            for f, want in POLICY_PUBLISH_FIXED.items():
                if given.get(f) != want:
                    bad.append(f"{profile}: {POLICY_PUBLISH} {f} is {given.get(f)!r}, not {want!r} (A445)")
            if given.get("--dsn-file") != f"/run/trust-dsn/{given.get('--tenant')}" or not str(given.get("--tenant", "")).startswith("ten_"):
                bad.append(f"{profile}: {POLICY_PUBLISH} must read the Trust login file of the tenant it publishes for (A356, A445)")
        deps = svc.get("depends_on") or {}
        needed = {MIGRATE: ("postgres",), TENANT_INIT: (TENANTS_PROVISION, OPERATOR_INIT, TRUST_KEYS_INIT), POLICY_PUBLISH: (TENANT_INIT, TRUST_DSN_INIT)}[name]
        for dep in needed:
            want = "service_healthy" if dep == "postgres" else "service_completed_successfully"
            if deps.get(dep, {}).get("condition") != want:
                bad.append(f"{profile}: {name} must wait for {dep} ({want}) (A527)")
    if name in PSQL_ONESHOTS:
        want = PSQL_ONESHOTS[name]
        if svc.get("image") != PSQL_IMAGE:
            bad.append(f"{profile}: {name} must run from the database image {PSQL_IMAGE} (no new image; A353)")
        if svc.get("entrypoint") != want["entrypoint"] or svc.get("command"):
            bad.append(f"{profile}: {name} must run exactly {want['entrypoint']} with no arguments (a password never goes in an argument; A527, A536)")
        want_user = bench_user() if want["runuser"] == "bench" else CORE_UID_USER
        if str(svc.get("user", "")) != want_user:
            bad.append(f"{profile}: {name} must run as {want_user}, never root (A353, A536)")
        if not any(str(t).startswith("/tmp") for t in (svc.get("tmpfs") or [])):
            bad.append(f"{profile}: {name} must have a tmpfs at /tmp (A536)")
        if set((svc.get("networks") or {}).keys() if isinstance(svc.get("networks"), dict) else svc.get("networks") or []) != {"data_net"}:
            bad.append(f"{profile}: {name} must be on data_net only (A353)")
        if [str(c).upper() for c in (svc.get("cap_drop") or [])] != ["ALL"] or svc.get("cap_add") or not svc.get("read_only"):
            bad.append(f"{profile}: {name} must drop ALL capabilities, add none and have a read-only root filesystem (A353)")
        fixed = dict(PSQL_ENV_FIXED, PGUSER=want["pguser"], PGPASSFILE=want["passfile"])
        extra = {"NOEMA_TENANTS"} | ({"NOEMA_EMITTERS"} if name == TENANTS_PROVISION else set())
        if not isinstance(env, dict) or set(env) != set(fixed) | extra or any(str(env.get(k)) != v for k, v in fixed.items()):
            bad.append(f"{profile}: {name}'s environment must be exactly {sorted(set(fixed) | extra)} with {fixed}: the login, the host and the password FILE, never a password (A527, A353)")
        deps = svc.get("depends_on") or {}
        for dep, cond in ((("postgres", "service_healthy"), (MIGRATE, "service_completed_successfully"), (OPERATOR_INIT, "service_completed_successfully")) if name == TENANTS_CREATE else ((TENANTS_CREATE, "service_completed_successfully"),)):
            if deps.get(dep, {}).get("condition") != cond:
                bad.append(f"{profile}: {name} must wait for {dep} ({cond}) (A353)")
        if set(held) != want["secrets"]:
            bad.append(f"{profile}: {name} must hold exactly {sorted(want['secrets'])} (A536)")
        script_mounts = [v for v in vols if str(v.get("target", "")) == want["script"] and v.get("read_only") and v.get("type") == "bind"]
        if len(script_mounts) != 1:
            bad.append(f"{profile}: {name} must mount its script read-only exactly once at {want['script']} (A353)")
    if name == "core":
        if (svc.get("depends_on") or {}).get(POLICY_PUBLISH, {}).get("condition") != "service_completed_successfully":
            bad.append(f"{profile}: the core must wait for {POLICY_PUBLISH} to complete successfully, or it starts with no policy in force (A445)")
        for volume in (OPERATOR_VOLUME, TRUST_KEYS_VOLUME):
            if any(v.get("source") == volume for v in vols):
                bad.append(f"{profile}: the core mounts {volume}; it holds neither the operator login nor the core-trust key (A527, INV-29)")
    return bad


def settings_order_problems(values):
    """The ordering constraints of A477, which the loaders enforce: the activity timeout and the recovery idle time must each exceed the investigation run timeout, or a live run could be picked
    up twice. A value that is not a whole number is a problem too."""
    try:
        run = int(values["NOEMA_INVESTIGATION_RUN_TIMEOUT_MS"])
        activity = int(values["NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS"])
        idle = int(values["NOEMA_STM_RECOVERY_IDLE_MS"])
    except (KeyError, ValueError):
        return ["the run timeout, the activity timeout and the recovery idle time must be whole numbers of milliseconds (A477)"]
    bad = []
    if activity <= run:
        bad.append("NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS must exceed NOEMA_INVESTIGATION_RUN_TIMEOUT_MS (A477)")
    if idle <= run:
        bad.append("NOEMA_STM_RECOVERY_IDLE_MS must exceed NOEMA_INVESTIGATION_RUN_TIMEOUT_MS (A477)")
    try:
        if int(values["NOEMA_TRUST_GRANT_LIFETIME_MS"]) > int(values["NOEMA_TRUST_RECORD_LIFETIME_MS"]):
            bad.append("NOEMA_TRUST_GRANT_LIFETIME_MS must not exceed NOEMA_TRUST_RECORD_LIFETIME_MS (A506, A503)")
    except (KeyError, ValueError):
        bad.append("the grant and record lifetimes must be whole numbers of milliseconds (A506)")
    return bad


# A506: the DSN directories and the key directories. Each is written once by its one-shot (trust-dsn-init for the six DSN volumes, core-keys-init for the five key volumes) and mounted read-only by the
# core and by nothing else; the five key values are held by core-keys-init only; the per-login emitter secrets are mounted by postgres and trust-dsn-init only.
KEYS_INIT = "core-keys-init"
DSN_VOLUMES = ("trust_dsns", "orchestrator_dsns", "record_read_dsns", "edge_dsns", "execution_dsns", "outcome_dsns", "evidence_dsns")
KEY_VOLUMES = ("orchestrator_keys", "edge_keys", "input_hash", "execution_keys", "outcome_keys", "evidence_keys")
KEY_SECRETS = ("core_workflows_ed25519_seed_1", "core_edge_ed25519_seed_1", "input_hash_key", "core_execution_ed25519_seed_1", "core_outcome_ed25519_seed_1", "core_evidence_ed25519_seed_1")
NEVER_SET = {"NOEMA_TEMPORAL_TLS_CERT_FILE": "the bench Temporal serves server-side TLS only (DEBT-46): the client certificate stays unset (A506)",
             "NOEMA_TEMPORAL_TLS_KEY_FILE": "the bench Temporal serves server-side TLS only (DEBT-46): the client key stays unset (A506)",
             "NOEMA_WRITE_SWAP_DECISIONS": "a test-only switch for the proposer-swap fixture, never a Compose setting (A506)"}
TRUST_DSN_INIT, TRUST_DSN_VOLUME, TENANT_LOGIN_SECRET = "trust-dsn-init", "trust_dsns", "noema_tenant_login_password"
# ENV-04 (A445, A484, A489, A527): the tenant-init and policy-publish one-shots. tenant-init registers the core-trust PUBLIC key with key_register, which only the owner and the superuser may call, so
# its login is the superuser's (the migration login's password belongs to the migrate one-shot and postgres alone, A527). The superuser password is held by postgres and by operator-dsn-init ONLY; that
# step writes the one DSN file into operator_dsn, which tenant-init alone mounts (read-only). The core-trust seed is held by core-trust-keys-init ONLY; it writes core_trust_keys, which tenant-init and
# policy-publish alone mount (read-only). The core mounts neither volume. Each of the two runs from the core image under its own entrypoint with exactly the flags below: no --tenant on tenant-init
# (tenant rows have one creator, A489), no --published-by on policy-publish (the publisher is the database role, A474), and no password, DSN or key in an argument or an environment variable.
OPERATOR_INIT, OPERATOR_VOLUME, SUPERUSER_SECRET = "operator-dsn-init", "operator_dsn", "postgres_superuser_password"
TRUST_KEYS_INIT, TRUST_KEYS_VOLUME, TRUST_KEY_SECRET = "core-trust-keys-init", "core_trust_keys", "core_trust_ed25519_seed_1"
TENANT_INIT, POLICY_PUBLISH = "tenant-init", "policy-publish"
# A536 (amends A527; A521, EL's flags): the migration login's password is held by postgres (the mode-644 original, `noema_migrator_password`) and by TWO one-shots, migrate and tenants-provision, which hold
# its private copy (`migrator_password_file`, mode 0400, owned by the host user) and run as that user: cmd/migrate refuses a password file that is not 0400 and owned by the user it runs as, and libpq refuses a
# password file that is group- or world-accessible. Each is a hardened one-shot: a read-only root, a tmpfs, no capability, no network but data_net (postgres), the password in no argument, no environment variable,
# no log and no error. migrate runs exactly the flags below and sets no environment variable; tenants-provision writes the one-line libpq file into its tmpfs from the private copy with the exact wrapper below.
MIGRATOR_SECRET, MIGRATOR_FILE_SECRET, MIGRATE = "noema_migrator_password", "migrator_password_file", "migrate"
MIGRATE_FLAGS = {"--host": "postgres", "--port": "5432", "--database": "noema", "--user": "noema_migration_login", "--password-file": "/run/secrets/migrator_password_file", "--sslmode": "prefer",
                 "--migrations-dir": "/migrations"}
PROVISION_WRAPPER = ["/bin/sh", "-c", "umask 077; { printf 'postgres:5432:noema:noema_migration_login:'; cat /run/secrets/migrator_password_file; } > /tmp/pgpass && exec sh /scripts/provision-tenants.sh"]
# The two psql one-shots (A353, A536, DEBT-04): create-tenants.sh as the superuser (the operator DSN step's one-line libpq file) and provision-tenants.sh as the migration login (the wrapper above), both from the
# database image, read-only root, tmpfs, no capability, data_net only. A password is never in an argument or an environment variable: PGPASSFILE names a file.
TENANTS_CREATE, TENANTS_PROVISION, PSQL_IMAGE = "tenants-create", "tenants-provision", "noema-postgres:dev"
CORE_UID_USER = "65532:65532"
PSQL_ONESHOTS = {
    TENANTS_CREATE: {"entrypoint": ["/bin/sh", "/scripts/create-tenants.sh"], "script": "/scripts/create-tenants.sh", "pguser": "postgres", "passfile": "/run/operator-dsn/pgpass", "runuser": "core", "secrets": set()},
    TENANTS_PROVISION: {"entrypoint": PROVISION_WRAPPER, "script": "/scripts/provision-tenants.sh", "pguser": "noema_migration_login", "passfile": "/tmp/pgpass", "runuser": "bench", "secrets": {MIGRATOR_FILE_SECRET}},
}
PSQL_ENV_FIXED = {"PGHOST": "postgres", "PGPORT": "5432", "PGDATABASE": "noema"}
ONESHOT_ENTRYPOINTS = {TENANT_INIT: "/usr/local/bin/tenantinit", POLICY_PUBLISH: "/usr/local/bin/policypublish", MIGRATE: "/usr/local/bin/migrate"}
TENANT_INIT_FLAGS = {"--dsn-file": "/run/operator-dsn/dsn", "--key-dir": "/run/core-trust-keys", "--key-max-active": "1"}
POLICY_PUBLISH_FLAGS = ("--tenant", "--pack-dir", "--pack-file", "--dsn-file", "--key-dir", "--key-max-active")
POLICY_PUBLISH_FIXED = {"--pack-dir": "/run/packs", "--pack-file": "fintech_v1.json", "--key-dir": "/run/core-trust-keys", "--key-max-active": "1"}


def bench_user():
    """The user the bench one-shots that hold the migration password run as: the host user make bench-up exports (NOEMA_BENCH_UID, NOEMA_BENCH_GID), which also owns the secret files. The netcheck sets the same
    placeholders the resolver needs, so the pin is checked against the resolved configuration."""
    return f"{BENCH_ENV['NOEMA_BENCH_UID']}:{BENCH_ENV['NOEMA_BENCH_GID']}"


def argv_of(svc):
    """The service's entrypoint and command as one list of strings; a string form is split on whitespace."""
    out = []
    for key in ("entrypoint", "command"):
        v = svc.get(key)
        out += v.split() if isinstance(v, str) else [str(x) for x in (v or [])]
    return out


def flags_of(argv):
    """The flags after the entrypoint as a list of (flag, value) pairs; a value-less or doubled flag shows as a pair with None. `--flag=value` splits at the first =."""
    pairs, i = [], 1
    while i < len(argv):
        tok = argv[i]
        if tok.startswith("-"):
            if "=" in tok:
                f, _, v = tok.partition("=")
                pairs.append((f, v))
                i += 1
            elif i + 1 < len(argv) and not argv[i + 1].startswith("-"):
                pairs.append((tok, argv[i + 1]))
                i += 2
            else:
                pairs.append((tok, None))
                i += 1
        else:
            pairs.append((None, tok))
            i += 1
    return pairs
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
SERVICE_CA_SOURCE, SERVICE_CA_TARGET = "secrets/tls/ca.crt", "/run/tls/ca.crt"  # A721: the service-to-service mTLS CA; the operator CA must be a different file
ADMIN_EMITTER_HOLDER = "ops"  # A699: the only service that may hold a tenant's admin emitter login; it does not exist yet (EL, G2)


def intron_overlay_text_problems(text):
    """The overlay's own text must set bind.create_host_path: false (Voice): Compose creates a missing host path as an EMPTY DIRECTORY, and the key reader would then refuse it at every call with
    no sign at start-up. It is read from the file, not from `docker compose config`, because older Compose versions do not print that key back."""
    uncommented = chr(10).join(line.split(" #", 1)[0] for line in text.splitlines() if not line.lstrip().startswith("#"))
    if not re.search(r"(?m)^\s+bind:[ \t]*" + chr(10) + r"\s+create_host_path:\s*false[ \t]*$", uncommented):
        return [f"{INTRON_OVERLAY}: the key bind must set bind.create_host_path: false, or a wrong host path becomes an empty directory at the key path (A379)"]
    return []


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
            if svc.get(flag) and not (flag == "cap_add" and name in (TRUST_DSN_INIT, KEYS_INIT, OPERATOR_INIT, TRUST_KEYS_INIT, TLS_INIT)):  # each one-shot adds exactly the set its own rule pins, checked by its own rule below
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
        # A667: the core's model-client key. Only the one-shot mounts the host file; the core reads the copy from a volume that only the one-shot writes.
        key_binds = [v for v in vols if str(v.get("source", "")).replace("\\", "/").rsplit("/", 1)[-1] == "core-client.key"]
        if key_binds and name != TLS_INIT:
            bad.append(f"{profile}: {name} mounts core-client.key from the host; only {TLS_INIT} may, and the core reads the copy it makes (A667)")
        kv = [v for v in vols if v.get("source") == TLS_KEY_VOLUME]
        if kv and name not in ("core", TLS_INIT):
            bad.append(f"{profile}: {name} mounts the {TLS_KEY_VOLUME} volume; only the core (read-only) and {TLS_INIT} may (INV-29, A667)")
        if name == "core":
            if len(kv) != 1 or not kv[0].get("read_only") or kv[0].get("target") != TLS_KEY_TARGET:
                bad.append(f"{profile}: the core must mount the {TLS_KEY_VOLUME} volume exactly once, read-only, at {TLS_KEY_TARGET} (A667)")
            if str((svc.get("environment") or {}).get(TLS_KEY_SETTING[0], "")) != TLS_KEY_SETTING[1]:
                bad.append(f"{profile}: the core's {TLS_KEY_SETTING[0]} must be {TLS_KEY_SETTING[1]} (the file the init step writes; A667)")
            if (svc.get("depends_on") or {}).get(TLS_INIT, {}).get("condition") != "service_completed_successfully":
                bad.append(f"{profile}: the core must wait for {TLS_INIT} to complete successfully, or it starts with a key file it refuses (A667)")
        if name == TLS_INIT:
            if len(key_binds) != 1 or not key_binds[0].get("read_only") or key_binds[0].get("target") != "/in/core-client.key" or key_binds[0].get("type") != "bind":
                bad.append(f"{profile}: {TLS_INIT} must bind the one host file core-client.key read-only at /in/core-client.key (A667)")
            writable = [v for v in vols if v.get("source") == TLS_KEY_VOLUME and not v.get("read_only")]
            if len(writable) != 1:
                bad.append(f"{profile}: {TLS_INIT} must mount {TLS_KEY_VOLUME} writable exactly once (A667)")
            if str(svc.get("user", "")) not in ("0:0", "0", "root"):
                bad.append(f"{profile}: {TLS_INIT} must run as root to set the key file owner (A667)")
            if svc.get("network_mode") != "none" or svc.get("networks"):
                bad.append(f"{profile}: {TLS_INIT} must have no network (A667)")
            if not svc.get("read_only"):
                bad.append(f"{profile}: {TLS_INIT} must have a read-only root (A667)")
            if set(svc.get("cap_drop") or []) != {"ALL"} or set(svc.get("cap_add") or []) != TLS_INIT_CAPS:
                bad.append(f"{profile}: {TLS_INIT} must drop ALL capabilities and add back exactly {sorted(TLS_INIT_CAPS)}, never DAC_OVERRIDE (A667)")
            if svc.get("secrets") or svc.get("ports"):
                bad.append(f"{profile}: {TLS_INIT} holds a secret or publishes a port; it holds the one host file only (A667)")
            if set(((svc.get("environment") or {}) if isinstance(svc.get("environment"), dict) else {}).get(k) for k in ("CORE_TLS_UID", "CORE_TLS_GID")) != {"65532"}:
                bad.append(f"{profile}: {TLS_INIT} must write the file for user and group 65532, the core's (A667)")
        # A356: the tenant login password and the DSN files it makes.
        if TENANT_LOGIN_SECRET in held and name not in ("postgres", TRUST_DSN_INIT):
            bad.append(f"{profile}: {name} holds {TENANT_LOGIN_SECRET}; only postgres and {TRUST_DSN_INIT} may (INV-29, A356)")
        dsn_mounts = [v for v in vols if v.get("source") == TRUST_DSN_VOLUME]
        if dsn_mounts and name not in ("core", TRUST_DSN_INIT, POLICY_PUBLISH):
            bad.append(f"{profile}: {name} mounts the {TRUST_DSN_VOLUME} volume; only the core (read-only), {POLICY_PUBLISH} (read-only) and {TRUST_DSN_INIT} may (INV-29, A356)")
        # A506: every DSN and key volume has one writer and one reader; the key values and the emitter secrets have their holders.
        for volume, writer in [(v, TRUST_DSN_INIT) for v in DSN_VOLUMES] + [(v, KEYS_INIT) for v in KEY_VOLUMES]:
            mounts = [v for v in vols if v.get("source") == volume]
            allowed = ("core", writer) + ((POLICY_PUBLISH,) if volume == TRUST_DSN_VOLUME else ())  # policy-publish reads the tenant's Trust login file, read-only (A445)
            if mounts and name not in allowed:
                bad.append(f"{profile}: {name} mounts the {volume} volume; only the core (read-only), {writer}{' and ' + POLICY_PUBLISH + ' (read-only)' if volume == TRUST_DSN_VOLUME else ''} may (INV-29, A506)")
            if name == POLICY_PUBLISH and volume == TRUST_DSN_VOLUME and (len(mounts) != 1 or not mounts[0].get("read_only")):
                bad.append(f"{profile}: {POLICY_PUBLISH} must mount the {volume} volume exactly once, read-only (A445)")
            if name == writer and len([m for m in mounts if not m.get("read_only")]) != 1:
                bad.append(f"{profile}: {writer} must mount {volume} writable exactly once (A506)")
            if name == "core" and profile == "bench" and (len(mounts) != 1 or not mounts[0].get("read_only")):
                bad.append(f"{profile}: the core must mount the {volume} volume exactly once, read-only (A506)")
        for secret in KEY_SECRETS:
            if secret in held and name != KEYS_INIT:
                bad.append(f"{profile}: {name} holds the secret {secret}; only {KEYS_INIT} may (INV-29, A506)")
        if any(str(v.get("source", "")).rstrip("/").endswith("emitter_logins") for v in vols) and name not in ("postgres", TRUST_DSN_INIT):
            bad.append(f"{profile}: {name} mounts the per-login emitter secrets; only postgres and {TRUST_DSN_INIT} may (INV-29, A506)")
        for setting, why in NEVER_SET.items():
            if setting in (svc.get("environment") or {}):
                bad.append(f"{profile}: {name} sets {setting}: {why}")
        if name == KEYS_INIT:
            if [str(c).upper() for c in (svc.get("cap_drop") or [])] != ["ALL"] or sorted(str(c).upper() for c in (svc.get("cap_add") or [])) != ["CHOWN", "DAC_READ_SEARCH", "FOWNER", "SETGID", "SETUID"]:
                bad.append(f"{profile}: {KEYS_INIT} must drop ALL capabilities and add back only CHOWN, DAC_READ_SEARCH, FOWNER, SETUID and SETGID: DAC_READ_SEARCH so it can READ the key secrets, which keep the host user's ownership and mode 0600 (Codex P1 on noema#874); never DAC_OVERRIDE (A506)")
            if set(held) != set(KEY_SECRETS):
                bad.append(f"{profile}: {KEYS_INIT} must hold exactly the five key secrets {list(KEY_SECRETS)} (A506)")
        if name == "core" and profile == "bench":
            for init in (TRUST_DSN_INIT, KEYS_INIT):
                if (svc.get("depends_on") or {}).get(init, {}).get("condition") != "service_completed_successfully":
                    bad.append(f"{profile}: the core must wait for {init} to complete successfully (A506)")
        if name == TRUST_DSN_INIT:
            if [str(c).upper() for c in (svc.get("cap_drop") or [])] != ["ALL"] or sorted(str(c).upper() for c in (svc.get("cap_add") or [])) != ["CHOWN", "FOWNER", "SETGID", "SETUID"]:
                bad.append(f"{profile}: {TRUST_DSN_INIT} must drop ALL capabilities and add back only CHOWN, FOWNER, SETUID and SETGID (SR on noema#790)")
            if set(held) != {TENANT_LOGIN_SECRET}:
                bad.append(f"{profile}: {TRUST_DSN_INIT} must hold the secret {TENANT_LOGIN_SECRET} and nothing else (A356)")
            if len([v for v in dsn_mounts if not v.get("read_only")]) != 1:
                bad.append(f"{profile}: {TRUST_DSN_INIT} must mount {TRUST_DSN_VOLUME} writable exactly once (A356)")
        if profile == "bench":
            bad.extend(oneshot_problems(profile, name, svc, held, vols))
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
        # A699, A714: a tenant's admin emitter login is held by the ops listener only. No other service mounts a volume, bind or secret, or sets an environment variable, whose name pairs "admin" with
        # "emitter" or "dsn". No service in the stack holds one today; the ops listener (EL, G2) is named `ADMIN_EMITTER_HOLDER`, and its PR changes the name there if it calls the service otherwise.
        admin_refs = [str(v.get("source", "")) + " " + str(v.get("target", "")) for v in (svc.get("volumes") or []) if isinstance(v, dict)]
        admin_refs += [str(x if isinstance(x, str) else x.get("source", "")) for x in (svc.get("secrets") or [])]
        admin_refs += list((svc.get("environment") or {}).keys()) if isinstance(svc.get("environment"), dict) else []
        if name != ADMIN_EMITTER_HOLDER and any(re.search(r"admin", r, re.I) and re.search(r"emitter|dsn", r, re.I) for r in admin_refs):
            bad.append(f"{profile}: {name} mounts or names an admin emitter login; only the ops listener may hold it (A699, INV-29)")
        # Intron's key (A249): a file mount read by the model service only. No service may carry it as an environment variable, and no service but the model may name a setting or a mount for it.
        env_names = set((svc.get("environment") or {}).keys()) if isinstance(svc.get("environment"), dict) else set()
        mounts = [str(v.get("source", "")) + " " + str(v.get("target", "")) for v in (svc.get("volumes") or [])] + [str(x if isinstance(x, str) else x.get("source", "")) for x in (svc.get("secrets") or [])]
        if "INTRON_API_KEY" in env_names:
            bad.append(f"{profile}: {name} carries the Intron key as an environment variable; it comes only from a file mount (A249)")
        if name != "model" and ("INTRON_API_KEY_FILE" in env_names or any("intron" in m.lower() for m in mounts)):
            bad.append(f"{profile}: {name} names or mounts Intron's key; only the model service may (A249, INV-29)")
        if name == "model" and str((svc.get("environment") or {}).get("NOEMA_MODEL_PROVIDER_PROFILE", "")) != "live":
            bad.append(f"{profile}: the model must set NOEMA_MODEL_PROVIDER_PROFILE to \"live\" in every profile; absent refuses the start and replay is chosen only by a later ruling, never by this file (A679, A695)")
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
        # A721: "any certificate the operator CA signs is an operator" holds only because the operator CA is a different CA from the service-to-service mTLS CA, so a service certificate can never act
        # as an operator. A service that sets NOEMA_OPS_CLIENT_CA_FILE must have a mount for that path whose source is not the service CA's file (secrets/tls/ca.crt), and the path must not be the path any
        # service reads the service CA at. No service sets it today (EL's ops listener, noema#1013, is not composed yet), so this holds without effect until it does.
        ops_ca = str((svc.get("environment") or {}).get("NOEMA_OPS_CLIENT_CA_FILE", "")) if isinstance(svc.get("environment"), dict) else ""
        if ops_ca:
            ops_mounts = [v for v in (svc.get("volumes") or []) if isinstance(v, dict) and str(v.get("target", "")) == ops_ca]
            service_ca_sources = {str(v.get("source", "")).replace("\\", "/") for v in (svc.get("volumes") or []) if isinstance(v, dict) and str(v.get("source", "")).replace("\\", "/").endswith(SERVICE_CA_SOURCE)}
            if len(ops_mounts) != 1:
                bad.append(f"{profile}: {name} sets NOEMA_OPS_CLIENT_CA_FILE to {ops_ca} but mounts nothing there (or more than one thing): the operator CA must be its own file (A721)")
            elif str(ops_mounts[0].get("source", "")).replace("\\", "/").endswith(SERVICE_CA_SOURCE) or ops_ca == SERVICE_CA_TARGET:
                bad.append(f"{profile}: {name}'s operator CA is the service-to-service mTLS CA; a service certificate would act as an operator (A721)")
            elif service_ca_sources and str(ops_mounts[0].get("source", "")) in service_ca_sources:
                bad.append(f"{profile}: {name}'s operator CA and the service CA are the same file (A721)")
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
        "the core mounts an admin emitter DSN directory": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "volume", "source": "admin_emitter_dsn", "target": "/run/admin-emitter-dsn", "read_only": True})),
        "the model names an admin emitter DSN setting": ("dev", lambda c: c["services"]["model"]["environment"].update({"NOEMA_ADMIN_EMITTER_DSN_DIR": "/x"})),
        "the model loses the provider profile": ("dev", lambda c: c["services"]["model"]["environment"].pop("NOEMA_MODEL_PROVIDER_PROFILE")),
        "the ci model's provider profile is replay": ("ci", lambda c: c["services"]["model"]["environment"].update({"NOEMA_MODEL_PROVIDER_PROFILE": "replay"})),
        "the demo model's provider profile is empty": ("demo", lambda c: c["services"]["model"]["environment"].update({"NOEMA_MODEL_PROVIDER_PROFILE": ""})),
        "the bench model's provider profile is upper case": ("bench", lambda c: c["services"]["model"]["environment"].update({"NOEMA_MODEL_PROVIDER_PROFILE": "LIVE"})),
        "the model carries the Intron key as a variable": ("dev", lambda c: c["services"]["model"]["environment"].update({"INTRON_API_KEY": "x"})),
        "the core is given the Intron key file": ("dev", lambda c: c["services"]["core"]["environment"].update({"INTRON_API_KEY_FILE": "/run/secrets/intron_api_key"})),
        "the UI mounts the Intron key file": ("dev", lambda c: c["services"]["ui"].setdefault("volumes", []).append({"type": "bind", "source": "/h/secrets/intron_api_key", "target": "/run/intron_api_key"})),
        "the bench model gets the key file variable but no mount": ("bench", lambda c: c["services"]["model"]["environment"].update({"INTRON_API_KEY_FILE": "/run/intron/key"})),
        "the bench model with the key mount loses the variable": ("bench+intron", lambda c: c["services"]["model"]["environment"].pop("INTRON_API_KEY_FILE")),
        "the bench key mount is writable": ("bench+intron", lambda c: [v.update({"read_only": False}) for v in c["services"]["model"]["volumes"] if v.get("target") == "/run/intron/key"]),
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
        # A667: the core's model-client key
        "the core mounts the host client key again": ("dev", lambda c: c["services"]["core"]["volumes"].append({"type": "bind", "source": "/h/secrets/tls/core-client.key", "target": "/run/tls/core-client.key", "read_only": True})),
        "the core's key setting points at the host path": ("ci", lambda c: c["services"]["core"]["environment"].update({"NOEMA_MODEL_TLS_KEY_FILE": "/run/tls/core-client.key"})),
        "the core drops the key volume": ("dev", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if not (isinstance(v, dict) and v.get("source") == "core_client_key")]})),
        "the core mounts the key volume writable": ("ci", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if isinstance(v, dict) and v.get("source") == "core_client_key"]),
        "the core mounts the key volume at another path": ("demo", lambda c: [v.update({"target": "/run/tls"}) for v in c["services"]["core"]["volumes"] if isinstance(v, dict) and v.get("source") == "core_client_key"]),
        "the core does not wait for the key step": ("dev", lambda c: c["services"]["core"]["depends_on"].pop("core-tls-init")),
        "the model mounts the key volume": ("dev", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "core_client_key", "target": "/k"})),
        "the model mounts the host client key": ("ci", lambda c: c["services"]["model"]["volumes"].append({"type": "bind", "source": "/h/secrets/tls/core-client.key", "target": "/k", "read_only": True})),
        "the key step mounts the volume read-only": ("dev", lambda c: [v.update({"read_only": True}) for v in c["services"]["core-tls-init"]["volumes"] if isinstance(v, dict) and v.get("source") == "core_client_key"]),
        "the key step mounts the host key writable": ("ci", lambda c: [v.update({"read_only": False}) for v in c["services"]["core-tls-init"]["volumes"] if isinstance(v, dict) and str(v.get("source", "")).endswith("core-client.key")]),
        "the key step mounts the host key at another path": ("dev", lambda c: [v.update({"target": "/in/other.key"}) for v in c["services"]["core-tls-init"]["volumes"] if isinstance(v, dict) and str(v.get("source", "")).endswith("core-client.key")]),
        "the key step has a network": ("bench", lambda c: c["services"]["core-tls-init"].update({"network_mode": "bridge"})),
        "the key step writable root": ("ci", lambda c: c["services"]["core-tls-init"].update({"read_only": False})),
        "the key step runs as the core's user": ("demo", lambda c: c["services"]["core-tls-init"].update({"user": "65532:65532"})),
        "the key step may override file permissions": ("dev", lambda c: c["services"]["core-tls-init"]["cap_add"].append("DAC_OVERRIDE")),
        "the key step cannot read a key it does not own": ("dev", lambda c: c["services"]["core-tls-init"]["cap_add"].remove("DAC_READ_SEARCH")),
        "the key step keeps every capability": ("ci", lambda c: c["services"]["core-tls-init"].update({"cap_drop": []})),
        "the key step holds a secret": ("dev", lambda c: c["services"]["core-tls-init"].update({"secrets": [{"source": "noema_core_password", "target": "p"}]})),
        "the key step writes for root": ("dev", lambda c: c["services"]["core-tls-init"]["environment"].update({"CORE_TLS_UID": "0"})),
        "the key step is dropped": ("ci", lambda c: c["services"].pop("core-tls-init")),
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
        "trust-dsn-init keeps every capability": ("bench", lambda c: c["services"]["trust-dsn-init"].pop("cap_drop")),
        "trust-dsn-init adds a capability it does not need": ("bench", lambda c: c["services"]["trust-dsn-init"]["cap_add"].append("DAC_OVERRIDE")),
        "trust-dsn-init loses a capability it needs": ("bench", lambda c: c["services"]["trust-dsn-init"]["cap_add"].remove("SETGID")),
        "trust-dsn-init is dropped": ("bench", lambda c: c["services"].pop("trust-dsn-init")),
        "a bench DSN volume is mounted by the model": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "edge_dsns", "target": "/x", "read_only": True})),
        "the bench core mounts a key volume writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if v.get("source") == "edge_keys"]),
        "the bench core drops the input hash volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "input_hash"]})),
        # A554 and A564 (lane 5): the new DSN and key volumes, the new key secrets, and the six settings, each pinned like A506's.
        "the bench core drops the execution DSN volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "execution_dsns"]})),
        "the bench core drops the outcome DSN volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "outcome_dsns"]})),
        "the bench core drops the receipt key volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "execution_keys"]})),
        "the bench core drops the verdict key volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "outcome_keys"]})),
        "the bench core mounts the receipt keys writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if v.get("source") == "execution_keys"]),
        "the bench core mounts the outcome logins writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if v.get("source") == "outcome_dsns"]),
        "the model mounts the receipt keys": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "execution_keys", "target": "/x", "read_only": True})),
        "the model mounts the verdict keys": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "outcome_keys", "target": "/x", "read_only": True})),
        "the model mounts the execution logins": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "execution_dsns", "target": "/x", "read_only": True})),
        "the ui mounts the outcome logins": ("bench", lambda c: c["services"]["ui"].setdefault("volumes", []).append({"type": "volume", "source": "outcome_dsns", "target": "/x", "read_only": True})),
        "the bench core drops the evidence DSN volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "evidence_dsns"]})),
        "the bench core drops the evidence key volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "evidence_keys"]})),
        "the bench core mounts the evidence keys writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if v.get("source") == "evidence_keys"]),
        "the bench core mounts the evidence logins writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["core"]["volumes"] if v.get("source") == "evidence_dsns"]),
        "the model mounts the evidence keys": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "evidence_keys", "target": "/x", "read_only": True})),
        "the model mounts the evidence logins": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "evidence_dsns", "target": "/x", "read_only": True})),
        "the core holds the evidence seed": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "core_evidence_ed25519_seed_1", "target": "x"})),
        "the bench core's evidence key overlap changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EVIDENCE_KEY_MAX_ACTIVE": "5"})),
        "the bench core loses the evidence record timeout": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_EVIDENCE_RECORD_TIMEOUT_MS")),
        "the bench core's evidence record timeout changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EVIDENCE_RECORD_TIMEOUT_MS": "60000"})),
        "the bench core loses the evidence key file": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_EVIDENCE_KEY_FILE")),
        "the bench core loses an injection limit": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_INJECTION_MAX_DEPTH")),
        "the bench core's injection pattern bound changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_INJECTION_MAX_PATTERNS": "6400"})),
        "the bench core's injection field cap changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_INJECTION_FIELD_CAP": "100000"})),
        "the core holds the receipt seed": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "core_execution_ed25519_seed_1", "target": "x"})),
        "the core holds the verdict seed": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "core_outcome_ed25519_seed_1", "target": "x"})),
        "core-keys-init loses the verdict seed": ("bench", lambda c: c["services"]["core-keys-init"].update({"secrets": [x for x in c["services"]["core-keys-init"]["secrets"] if (x if isinstance(x, str) else x.get("source")) != "core_outcome_ed25519_seed_1"]})),
        "the bench core's execution key overlap changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EXECUTION_KEY_MAX_ACTIVE": "5"})),
        "the bench core's outcome key overlap changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_OUTCOME_KEY_MAX_ACTIVE": "1"})),
        "the bench core loses the execution DSN setting": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_EXECUTION_DSN_DIR")),
        "the bench core loses the outcome DSN setting": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_OUTCOME_DSN_DIR")),
        "the bench core's receipt key directory moves": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EXECUTION_KEY_DIR": "/run/outcome-keys"})),
        "the bench core shares one key directory for both signers": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_OUTCOME_KEY_DIR": "/run/execution-keys"})),
        "the bench core drops a DSN volume": ("bench", lambda c: c["services"]["core"].update({"volumes": [v for v in c["services"]["core"]["volumes"] if v.get("source") != "orchestrator_dsns"]})),
        "the bench core does not wait for core-keys-init": ("bench", lambda c: c["services"]["core"]["depends_on"].pop("core-keys-init")),
        "the core holds a key secret": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "input_hash_key", "target": "x"})),
        "core-keys-init also holds the core password": ("bench", lambda c: c["services"]["core-keys-init"]["secrets"].append({"source": "noema_core_password", "target": "x"})),
        "core-keys-init loses a secret": ("bench", lambda c: c["services"]["core-keys-init"]["secrets"].pop()),
        "core-keys-init cannot read a secret it does not own": ("bench", lambda c: c["services"]["core-keys-init"]["cap_add"].remove("DAC_READ_SEARCH")),
        "core-keys-init may override file permissions": ("bench", lambda c: c["services"]["core-keys-init"]["cap_add"].append("DAC_OVERRIDE")),
        "core-keys-init keeps every capability": ("bench", lambda c: c["services"]["core-keys-init"].pop("cap_drop")),
        "core-keys-init is dropped": ("bench", lambda c: c["services"].pop("core-keys-init")),
        # ENV-04 (A445, A484, A489, A527): who holds the superuser's password and the core-trust seed, who reads the operator DSN and the core-trust keys, and the exact arguments of the two one-shots.
        "the core holds the superuser's password": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "postgres_superuser_password", "target": "x"})),
        "tenant-init holds the superuser's password itself": ("bench", lambda c: c["services"]["tenant-init"].setdefault("secrets", []).append({"source": "postgres_superuser_password", "target": "x"})),
        "policy-publish holds the superuser's password": ("bench", lambda c: c["services"]["policy-publish"].setdefault("secrets", []).append({"source": "postgres_superuser_password", "target": "x"})),
        "the model names the superuser's password": ("bench", lambda c: c["services"]["model"].setdefault("environment", {}).update({"POSTGRES_SUPERUSER_PASSWORD_FILE": "/x"})),
        "operator-dsn-init also holds the core password": ("bench", lambda c: c["services"]["operator-dsn-init"]["secrets"].append({"source": "noema_core_password", "target": "x"})),
        "operator-dsn-init loses its secret": ("bench", lambda c: c["services"]["operator-dsn-init"].update({"secrets": []})),
        "operator-dsn-init may override file permissions": ("bench", lambda c: c["services"]["operator-dsn-init"]["cap_add"].append("DAC_OVERRIDE")),
        "operator-dsn-init has a network": ("bench", lambda c: c["services"]["operator-dsn-init"].update({"network_mode": "bridge"})),
        "operator-dsn-init is dropped": ("bench", lambda c: c["services"].pop("operator-dsn-init")),
        "the core holds the core-trust seed": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "core_trust_ed25519_seed_1", "target": "x"})),
        "tenant-init holds the core-trust seed": ("bench", lambda c: c["services"]["tenant-init"].setdefault("secrets", []).append({"source": "core_trust_ed25519_seed_1", "target": "x"})),
        "core-trust-keys-init also holds an input hash key": ("bench", lambda c: c["services"]["core-trust-keys-init"]["secrets"].append({"source": "input_hash_key", "target": "x"})),
        "core-trust-keys-init may override file permissions": ("bench", lambda c: c["services"]["core-trust-keys-init"]["cap_add"].append("DAC_OVERRIDE")),
        "core-trust-keys-init cannot read its seed": ("bench", lambda c: c["services"]["core-trust-keys-init"]["cap_add"].remove("DAC_READ_SEARCH")),
        "the core mounts the operator DSN": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "volume", "source": "operator_dsn", "target": "/x", "read_only": True})),
        "policy-publish mounts the operator DSN": ("bench", lambda c: c["services"]["policy-publish"]["volumes"].append({"type": "volume", "source": "operator_dsn", "target": "/x", "read_only": True})),
        "tenant-init mounts the operator DSN writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["tenant-init"]["volumes"] if v.get("source") == "operator_dsn"]),
        "tenant-init loses the operator DSN": ("bench", lambda c: c["services"]["tenant-init"].update({"volumes": [v for v in c["services"]["tenant-init"]["volumes"] if v.get("source") != "operator_dsn"]})),
        "the core mounts the core-trust keys": ("bench", lambda c: c["services"]["core"]["volumes"].append({"type": "volume", "source": "core_trust_keys", "target": "/x", "read_only": True})),
        "the model mounts the core-trust keys": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "volume", "source": "core_trust_keys", "target": "/x", "read_only": True})),
        "policy-publish mounts the core-trust keys writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["policy-publish"]["volumes"] if v.get("source") == "core_trust_keys"]),
        "policy-publish loses the core-trust keys": ("bench", lambda c: c["services"]["policy-publish"].update({"volumes": [v for v in c["services"]["policy-publish"]["volumes"] if v.get("source") != "core_trust_keys"]})),
        "tenant-init is given a tenant": ("bench", lambda c: c["services"]["tenant-init"]["command"].extend(["--tenant", "ten_alpha"])),
        "tenant-init is given a tenant with an equals sign": ("bench", lambda c: c["services"]["tenant-init"]["command"].append("--tenant=ten_alpha")),
        "tenant-init reads another DSN file": ("bench", lambda c: c["services"]["tenant-init"].update({"command": ["--dsn-file", "/run/trust-dsn/ten_alpha", "--key-dir", "/run/core-trust-keys", "--key-max-active", "1"]})),
        "tenant-init takes the DSN in the argument": ("bench", lambda c: c["services"]["tenant-init"].update({"command": ["--dsn-file=postgres://postgres:pw@postgres/noema", "--key-dir", "/run/core-trust-keys", "--key-max-active", "1"]})),
        "tenant-init allows three active keys": ("bench", lambda c: c["services"]["tenant-init"].update({"command": ["--dsn-file", "/run/operator-dsn/dsn", "--key-dir", "/run/core-trust-keys", "--key-max-active", "3"]})),
        "tenant-init runs as root": ("bench", lambda c: c["services"]["tenant-init"].update({"user": "0:0"})),
        "tenant-init joins the service network": ("bench", lambda c: c["services"]["tenant-init"]["networks"].update({"svc_net": None})),
        "tenant-init keeps a capability": ("bench", lambda c: c["services"]["tenant-init"].update({"cap_add": ["NET_ADMIN"]})),
        "tenant-init has a writable root": ("bench", lambda c: c["services"]["tenant-init"].update({"read_only": False})),
        "tenant-init runs a shell": ("bench", lambda c: c["services"]["tenant-init"].update({"entrypoint": ["/bin/sh"]})),
        "tenant-init carries the password in its environment": ("bench", lambda c: c["services"]["tenant-init"].setdefault("environment", {}).update({"PGPASSWORD": "x"})),
        "tenant-init does not wait for the operator DSN": ("bench", lambda c: c["services"]["tenant-init"]["depends_on"].pop("operator-dsn-init")),
        "tenant-init does not wait for the core-trust key": ("bench", lambda c: c["services"]["tenant-init"]["depends_on"].pop("core-trust-keys-init")),
        "tenant-init is dropped": ("bench", lambda c: c["services"].pop("tenant-init")),
        "policy-publish is given a publisher": ("bench", lambda c: c["services"]["policy-publish"]["command"].extend(["--published-by", "x"])),
        "policy-publish is given a publisher with an equals sign": ("bench", lambda c: c["services"]["policy-publish"]["command"].append("--published-by=x")),
        "policy-publish loses the tenant": ("bench", lambda c: c["services"]["policy-publish"].update({"command": [t for i, t in enumerate(c["services"]["policy-publish"]["command"]) if t not in ("--tenant", "ten_alpha")]})),
        "policy-publish reads another tenant's login file": ("bench", lambda c: c["services"]["policy-publish"].update({"command": ["--tenant", "ten_alpha", "--pack-dir", "/run/packs", "--pack-file", "fintech_v1.json", "--dsn-file", "/run/trust-dsn/ten_other", "--key-dir", "/run/core-trust-keys", "--key-max-active", "1"]})),
        "policy-publish publishes another pack file": ("bench", lambda c: c["services"]["policy-publish"].update({"command": ["--tenant", "ten_alpha", "--pack-dir", "/run/packs", "--pack-file", "other.json", "--dsn-file", "/run/trust-dsn/ten_alpha", "--key-dir", "/run/core-trust-keys", "--key-max-active", "1"]})),
        "policy-publish mounts the Trust logins writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["policy-publish"]["volumes"] if v.get("source") == "trust_dsns"]),
        "policy-publish runs as root": ("bench", lambda c: c["services"]["policy-publish"].update({"user": "0:0"})),
        "policy-publish does not wait for tenant-init": ("bench", lambda c: c["services"]["policy-publish"]["depends_on"].pop("tenant-init")),
        "the core does not wait for policy-publish": ("bench", lambda c: c["services"]["core"]["depends_on"].pop("policy-publish")),
        "policy-publish is dropped": ("bench", lambda c: c["services"].pop("policy-publish")),
        # A527 (A521): the migrate one-shot and the migration login's password. One mutation per leak path: an argument, the environment, a second reader or holder.
        "the core holds the migration password": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "noema_migrator_password", "target": "x"})),
        "tenant-init holds the migration password": ("bench", lambda c: c["services"]["tenant-init"].setdefault("secrets", []).append({"source": "noema_migrator_password", "target": "x"})),
        "migrate holds the migration password itself": ("bench", lambda c: c["services"]["migrate"].setdefault("secrets", []).append({"source": "noema_migrator_password", "target": "x"})),
        "operator-dsn-init holds the migration password": ("bench", lambda c: c["services"]["operator-dsn-init"]["secrets"].append({"source": "noema_migrator_password", "target": "x"})),
        "the model names the migration password": ("bench", lambda c: c["services"]["model"].setdefault("environment", {}).update({"MIGRATOR_PASSWORD_FILE": "/x"})),
        "migrate takes the password in an argument": ("bench", lambda c: c["services"]["migrate"]["command"].extend(["--password", "x"])),
        "migrate takes the password in an argument with an equals sign": ("bench", lambda c: c["services"]["migrate"]["command"].append("--password=x")),
        "migrate takes a DSN in an argument": ("bench", lambda c: c["services"]["migrate"]["command"].extend(["--dsn", "postgres://u:p@postgres/noema"])),
        "migrate takes a DSN file": ("bench", lambda c: c["services"]["migrate"]["command"].extend(["--dsn-file", "/run/x"])),
        "migrate reads another password file": ("bench", lambda c: c["services"]["migrate"].update({"command": [("/run/operator-dsn/dsn" if t == "/run/secrets/migrator_password_file" else t) for t in c["services"]["migrate"]["command"]]})),
        "migrate reads the world-readable original": ("bench", lambda c: c["services"]["migrate"].update({"command": [("/run/secrets/noema_migrator_password" if t == "/run/secrets/migrator_password_file" else t) for t in c["services"]["migrate"]["command"]]})),
        "migrate logs in as the superuser": ("bench", lambda c: c["services"]["migrate"].update({"command": [("postgres" if t == "noema_migration_login" else t) for t in c["services"]["migrate"]["command"]]})),
        "migrate verifies nothing about the server": ("bench", lambda c: c["services"]["migrate"].update({"command": [("allow" if t == "prefer" else t) for t in c["services"]["migrate"]["command"]]})),
        "migrate carries the password in its environment": ("bench", lambda c: c["services"]["migrate"].setdefault("environment", {}).update({"PGPASSWORD": "x"})),
        "migrate carries a password file path in its environment": ("bench", lambda c: c["services"]["migrate"].setdefault("environment", {}).update({"MIGRATOR_PASSWORD_FILE": "/run/migrator-password/password"})),
        "migrate mounts the migrations folder writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["migrate"]["volumes"] if v.get("target") == "/migrations"]),
        "migrate runs as root": ("bench", lambda c: c["services"]["migrate"].update({"user": "0:0"})),
        "migrate joins the service network": ("bench", lambda c: c["services"]["migrate"]["networks"].update({"svc_net": None})),
        "migrate keeps a capability": ("bench", lambda c: c["services"]["migrate"].update({"cap_add": ["NET_ADMIN"]})),
        "migrate runs a shell": ("bench", lambda c: c["services"]["migrate"].update({"entrypoint": ["/bin/sh"]})),
        "migrate does not wait for the database": ("bench", lambda c: c["services"]["migrate"]["depends_on"].pop("postgres")),
        "tenant-init does not wait for the tenants": ("bench", lambda c: c["services"]["tenant-init"]["depends_on"].pop("tenants-provision")),
        # The tenant steps (A353, DEBT-04): the sequence is migrate, tenants-create, tenants-provision, tenant-init, policy-publish, core. One mutation per leak path of a password: an argument, the
        # environment, a second reader of either password file, a wrong login.
        "tenants-create carries the superuser's password in its environment": ("bench", lambda c: c["services"]["tenants-create"]["environment"].update({"PGPASSWORD": "x"})),
        "tenants-provision carries the migration password in its environment": ("bench", lambda c: c["services"]["tenants-provision"]["environment"].update({"PGPASSWORD": "x"})),
        "tenants-create takes a password in an argument": ("bench", lambda c: c["services"]["tenants-create"].update({"command": ["--password", "x"]})),
        "tenants-provision runs a shell command string": ("bench", lambda c: c["services"]["tenants-provision"].update({"entrypoint": ["/bin/sh", "-c", "PGPASSWORD=x /scripts/provision-tenants.sh"]})),
        "tenants-create reads another password file": ("bench", lambda c: c["services"]["tenants-create"]["environment"].update({"PGPASSFILE": "/run/migrator-password/pgpass"})),
        "tenants-provision reads the superuser's password file": ("bench", lambda c: c["services"]["tenants-provision"]["environment"].update({"PGPASSFILE": "/run/operator-dsn/pgpass"})),
        "tenants-create logs in as the migration login": ("bench", lambda c: c["services"]["tenants-create"]["environment"].update({"PGUSER": "noema_migration_login"})),
        "tenants-provision logs in as the superuser": ("bench", lambda c: c["services"]["tenants-provision"]["environment"].update({"PGUSER": "postgres"})),
        "tenants-create holds the superuser's password itself": ("bench", lambda c: c["services"]["tenants-create"].setdefault("secrets", []).append({"source": "postgres_superuser_password", "target": "x"})),
        "tenants-provision holds the migration password itself": ("bench", lambda c: c["services"]["tenants-provision"].setdefault("secrets", []).append({"source": "noema_migrator_password", "target": "x"})),
        "tenants-provision mounts the operator DSN": ("bench", lambda c: c["services"]["tenants-provision"]["volumes"].append({"type": "volume", "source": "operator_dsn", "target": "/x", "read_only": True})),
        "tenants-create mounts the operator DSN writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["tenants-create"]["volumes"] if v.get("source") == "operator_dsn"]),
        "tenants-create runs as root": ("bench", lambda c: c["services"]["tenants-create"].update({"user": "0:0"})),
        "tenants-provision joins the service network": ("bench", lambda c: c["services"]["tenants-provision"]["networks"].update({"svc_net": None})),
        "tenants-create keeps a capability": ("bench", lambda c: c["services"]["tenants-create"].update({"cap_add": ["NET_ADMIN"]})),
        "tenants-provision has a writable root": ("bench", lambda c: c["services"]["tenants-provision"].update({"read_only": False})),
        "tenants-create runs another image": ("bench", lambda c: c["services"]["tenants-create"].update({"image": "postgres:17"})),
        "tenants-create mounts its script writable": ("bench", lambda c: [v.update({"read_only": False}) for v in c["services"]["tenants-create"]["volumes"] if v.get("target") == "/scripts/create-tenants.sh"]),
        "tenants-create does not wait for migrate": ("bench", lambda c: c["services"]["tenants-create"]["depends_on"].pop("migrate")),
        "tenants-create does not wait for the operator DSN": ("bench", lambda c: c["services"]["tenants-create"]["depends_on"].pop("operator-dsn-init")),
        "tenants-provision does not wait for the tenant rows": ("bench", lambda c: c["services"]["tenants-provision"]["depends_on"].pop("tenants-create")),
        "tenants-create is dropped": ("bench", lambda c: c["services"].pop("tenants-create")),
        # A536: the two holders of the migration password. One mutation per leak path: a third holder, a third reader, the password in an argument or the environment, a weaker one-shot.
        "the core holds the private copy of the migration password": ("bench", lambda c: c["services"]["core"].setdefault("secrets", []).append({"source": "migrator_password_file", "target": "x"})),
        "tenant-init holds the private copy of the migration password": ("bench", lambda c: c["services"]["tenant-init"].setdefault("secrets", []).append({"source": "migrator_password_file", "target": "x"})),
        "tenants-create holds the private copy of the migration password": ("bench", lambda c: c["services"]["tenants-create"].setdefault("secrets", []).append({"source": "migrator_password_file", "target": "x"})),
        "policy-publish holds the private copy of the migration password": ("bench", lambda c: c["services"]["policy-publish"].setdefault("secrets", []).append({"source": "migrator_password_file", "target": "x"})),
        "postgres holds the private copy of the migration password": ("bench", lambda c: c["services"]["postgres"].setdefault("secrets", []).append({"source": "migrator_password_file", "target": "x"})),
        "the model names the private copy of the migration password": ("bench", lambda c: c["services"]["model"].setdefault("environment", {}).update({"X_MIGRATOR_PASSWORD_FILE": "/run/secrets/migrator_password_file"})),
        "migrate holds the original as well": ("bench", lambda c: c["services"]["migrate"]["secrets"].append({"source": "noema_migrator_password", "target": "x"})),
        "migrate loses its secret": ("bench", lambda c: c["services"]["migrate"].update({"secrets": []})),
        "tenants-provision loses its secret": ("bench", lambda c: c["services"]["tenants-provision"].update({"secrets": []})),
        "migrate runs as the core's user": ("bench", lambda c: c["services"]["migrate"].update({"user": "65532:65532"})),
        "migrate runs as root": ("bench", lambda c: c["services"]["migrate"].update({"user": "0:0"})),
        "tenants-provision runs as the core's user": ("bench", lambda c: c["services"]["tenants-provision"].update({"user": "65532:65532"})),
        "migrate loses its tmpfs": ("bench", lambda c: c["services"]["migrate"].pop("tmpfs")),
        "tenants-provision loses its tmpfs": ("bench", lambda c: c["services"]["tenants-provision"].pop("tmpfs")),
        "migrate has a writable root": ("bench", lambda c: c["services"]["migrate"].update({"read_only": False})),
        "tenants-provision's wrapper puts the password in an argument": ("bench", lambda c: c["services"]["tenants-provision"].update({"entrypoint": ["/bin/sh", "-c", "PGPASSWORD=$(cat /run/secrets/migrator_password_file) exec sh /scripts/provision-tenants.sh"]})),
        "tenants-provision's wrapper leaves the file world-readable": ("bench", lambda c: c["services"]["tenants-provision"].update({"entrypoint": [("umask 022; " + t[len("umask 077; "):]) if t.startswith("umask 077; ") else t for t in c["services"]["tenants-provision"]["entrypoint"]]})),
        "tenants-provision's wrapper writes outside the tmpfs": ("bench", lambda c: c["services"]["tenants-provision"].update({"entrypoint": [t.replace("/tmp/pgpass", "/scripts/pgpass") for t in c["services"]["tenants-provision"]["entrypoint"]]})),
        "tenants-provision's wrapper logs in as the superuser": ("bench", lambda c: c["services"]["tenants-provision"].update({"entrypoint": [t.replace("noema_migration_login", "postgres") for t in c["services"]["tenants-provision"]["entrypoint"]]})),
        "tenants-provision reads another password file path": ("bench", lambda c: c["services"]["tenants-provision"]["environment"].update({"PGPASSFILE": "/run/secrets/migrator_password_file"})),
        "tenants-provision is dropped": ("bench", lambda c: c["services"].pop("tenants-provision")),
        "migrate is dropped": ("bench", lambda c: c["services"].pop("migrate")),
        "the model mounts the per-login emitter secrets": ("bench", lambda c: c["services"]["model"].setdefault("volumes", []).append({"type": "bind", "source": "/h/secrets/emitter_logins", "target": "/x", "read_only": True})),
        "the core sets the Temporal client certificate": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TEMPORAL_TLS_CERT_FILE": "/x"})),
        "the core sets the test-only swap switch": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_WRITE_SWAP_DECISIONS": "1"})),
        "the bench core's grant lifetime exceeds its record lifetime": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TRUST_GRANT_LIFETIME_MS": "700000"})),
        "the bench core's escalation graph changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TRUST_ESCALATION_GRAPH": "fin.graph.other"})),
        "the bench core's Temporal task queue changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TEMPORAL_TASK_QUEUE": "default"})),
        "the bench core's input dedup window changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_INPUT_DEDUP_WINDOW": "24h"})),
        "the bench core runs two investigation workers": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_INVESTIGATION_WORKERS": "2"})),
        "the bench core's activity timeout drops below the run timeout": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS": "100000"})),
        "the bench core loses an events setting": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_EVENTS_MAX_STREAMS_PER_PRINCIPAL")),
        "the bench core's events stream limit changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EVENTS_MAX_STREAMS": "1000"})),
        "the bench core's events event bound changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EVENTS_MAX_EVENT_BYTES": "1048576"})),
        "the bench core's execute timeout changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_EXECUTION_EXECUTE_TIMEOUT_MS": "60000"})),
        "the bench core loses the rollback timeout": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_EXECUTION_ROLLBACK_TIMEOUT_MS")),
        "the bench core's plan text bound changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_PLAN_TEXT_MAX_BYTES": "4096"})),
        "the bench core loses the recovery batch size": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_STM_RECOVERY_BATCH_SIZE")),
        "the bench core's pack size bound changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_PACK_MAX_BYTES": "4194304"})),
        "the bench core loses the pack manifest setting": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_PACK_MANIFEST")),
        "the bench core's store timeout changes": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_STORE_TIMEOUT_MS": "5000"})),
        "the bench core loses a settings-table value": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_DB_SERIALIZATION_ATTEMPTS")),
        "the bench core sets the at-risk limit itself": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_TRUST_AT_RISK_BPS": "2500"})),
        "the bench core loses the evidence payload bound": ("bench", lambda c: c["services"]["core"]["environment"].pop("NOEMA_PAYLOAD_MAX_BYTES")),
        "the core loses the model response bound": ("dev", lambda c: c["services"]["core"]["environment"].pop("NOEMA_MODEL_MAX_RESPONSE_BYTES")),
        "the core loses the model client certificate setting": ("ci", lambda c: c["services"]["core"]["environment"].pop("NOEMA_MODEL_TLS_CERT_FILE")),
        "the core uses the service CA as the operator CA": ("bench", lambda c: [c["services"]["core"]["environment"].update({"NOEMA_OPS_CLIENT_CA_FILE": "/run/ops/ca.crt"}), c["services"]["core"]["volumes"].append({"type": "bind", "source": "/h/secrets/tls/ca.crt", "target": "/run/ops/ca.crt", "read_only": True})]),
        "the core reads the operator CA at the service CA path": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_OPS_CLIENT_CA_FILE": "/run/tls/ca.crt"})),
        "the core names an operator CA it does not mount": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_OPS_CLIENT_CA_FILE": "/run/ops/ca.crt"})),
        "the core's model URL is empty": ("bench", lambda c: c["services"]["core"]["environment"].update({"NOEMA_MODEL_URL": ""})),
        "the stand-in leaves bench_net": ("ci", lambda c: c["services"]["standin"]["networks"].pop("bench_net")),
    }
    missed = []
    overlay_text = (COMPOSE_FILE.parent / INTRON_OVERLAY).read_text(encoding="utf-8")
    if intron_overlay_text_problems("") == [] or intron_overlay_text_problems("services: {}") == []:
        missed.append("the overlay text rule passes on no input or on a file without the key (it must fail)")
    for label, mutant in {"the overlay drops create_host_path": overlay_text.replace("create_host_path: false", "create_host_path: true"),
                          "the overlay has create_host_path only in a comment": overlay_text.replace("bind:" + chr(10) + "          create_host_path: false", "# bind:" + chr(10) + "          # create_host_path: false")}.items():
        if mutant == overlay_text or not intron_overlay_text_problems(mutant):
            missed.append(f"the overlay text rule did not fire for: {label}")
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
    found = check(configs["bench+intron"], "bench") + intron_overlay_text_problems((COMPOSE_FILE.parent / INTRON_OVERLAY).read_text(encoding="utf-8"))
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
