"""The compile service, built from a mounted configuration file, and only on a listener that requires a client certificate (UND-05; REQ-INT-001, REQ-INT-002, REQ-API-005, REQ-SEC-002; INV-02, INV-12, INV-29; AGENT-16).

Nothing here is on by default. The process builds a compile service only when `NOEMA_MODEL_COMPILE_CONFIG` names a file; without it the route answers 503, as before. With it, every member of the file
is required (no default in code: the timeout, the size bounds and the Evidence link are numbers the deployment sets, AGENT-16), and the service refuses to start unless its own listener settings
require a client certificate (`require_mtls_listener`): the Security Reviewer's condition on noema#544 and noema#555, that no compiler is wired on a plain listener. The check reads the settings the
image sets for uvicorn (`UVICORN_SSL_*`, noema#568); it cannot see the socket, so it is a refusal to start on a configuration that is not mutual TLS, not a proof of the peer's identity.

`DerivedMinter` is the default for the identifiers nobody owns yet (noema#580, an open question: it is the smallest safe default and changes in one place when the Architect answers). It uses no clock and
no randomness: the same request gives the same ids (INV-21), and the ids hold no digit, so the Record's guard against digit runs never meets one in them.
"""

import hashlib
import json
import re
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from noema_model.evidence.client import EvidenceClient, EvidenceClientConfig
from noema_model.evidence.transport import EvidenceTransport, UrllibEvidenceTransport, evidence_pool_size
from noema_model.harness import Harness
from noema_model.platform.config import ConfigError, reject_unknown, require_int, require_mapping, require_str
from noema_model.platform.mtls import client_ssl_context
from noema_model.understanding.compiler.assemble import Identifiers
from noema_model.understanding.compiler.compiler import PipelineCompiler
from noema_model.understanding.compiler.lexicon import load_pack_lexicons
from noema_model.understanding.compiler.packconfig import load_pipeline_config
from noema_model.understanding.compiler.service import CompileService
from noema_model.understanding.gate import UNSET, Node, PackView
from noema_model.understanding.pipeline import NodeCatalogue

COMPILE_CONFIG_ENV = "NOEMA_MODEL_COMPILE_CONFIG"
_MEMBERS = frozenset({"pack_file", "lexicon_dir", "out_of_scope_node", "timeout_s", "max_proposal_bytes", "max_request_bytes", "evidence", "evidence_tls"})
_TLS_MEMBERS = frozenset({"cert_file", "key_file", "ca_file"})
_PACK_ID = re.compile(r"[a-z][a-z0-9]*")
_PACK_VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
_ID_LETTERS = 26  # a 26-letter id carries about 122 bits of the request's hash; the contract allows 2 to 32 letters or digits
_MTLS_MESSAGE = "the compile service is built only on a listener that requires a client certificate (mutual TLS): set the UVICORN_SSL_* options of noema#568"


def compile_requested(environ: Mapping[str, str]) -> bool:
    return COMPILE_CONFIG_ENV in environ


def require_mtls_listener(environ: Mapping[str, str]) -> None:
    """Refuse unless the process's own listener settings require a client certificate: `UVICORN_SSL_CERT_REQS` exactly `2` (CERT_REQUIRED) and a certificate, key and CA set."""
    if environ.get("UVICORN_SSL_CERT_REQS") != "2" or not all(environ.get(name) for name in ("UVICORN_SSL_CERTFILE", "UVICORN_SSL_KEYFILE", "UVICORN_SSL_CA_CERTS")):
        raise ConfigError(_MTLS_MESSAGE)


class _Repeated(ValueError):
    pass


def _pairs(items: list[tuple[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in items:
        if key in out:
            raise _Repeated(key)
        out[key] = value
    return out


def _read_json(path: str, where: str) -> Any:
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"), object_pairs_hook=_pairs)
    except (OSError, ValueError, RecursionError) as err:
        raise ConfigError(f"{where}: unreadable, not JSON, or a member repeated") from err


def _positive_number(source: Mapping[str, Any], key: str) -> float:
    value = source.get(key)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not value > 0 or value != value or value == float("inf"):
        raise ConfigError(f"compile.{key}: expected a positive finite number")
    return float(value)


class _View:
    """The pack as the schema gate needs it: its reference and each node's slots by type. The model never proposes an outcome, so no node holds an outcome definition (INV-01)."""

    def __init__(self, pack_id: str, version: str, nodes: Mapping[str, Node]) -> None:
        self._ref = (pack_id, version)
        self._nodes = dict(nodes)

    def ref(self) -> tuple[str, str]:
        return self._ref

    def node(self, node_id: str) -> Node | None:
        return self._nodes.get(node_id)


class PackFileResolver:
    """The one pack this process holds, by `<pack_id>@<pack_version>` read from the pack file itself. Any other reference is None."""

    def __init__(self, pack_ref: str, view: PackView) -> None:
        self.pack_ref = pack_ref
        self._view = view

    @classmethod
    def from_file(cls, path: Path, *, out_of_scope_node: str) -> "PackFileResolver":
        pack = _read_json(str(path), "compile.pack_file")
        return cls.from_mapping(require_mapping(pack, "compile.pack_file"), out_of_scope_node=out_of_scope_node)

    @classmethod
    def from_mapping(cls, pack: Mapping[str, Any], *, out_of_scope_node: str) -> "PackFileResolver":
        pack_id, version = require_str(pack, "pack_id", "pack"), require_str(pack, "pack_version", "pack")
        if _PACK_ID.fullmatch(pack_id) is None or _PACK_VERSION.fullmatch(version) is None:
            raise ConfigError("pack: pack_id and pack_version must be <name> and <x.y.z>")
        catalogue = NodeCatalogue.from_pack(pack, out_of_scope_node=out_of_scope_node)
        nodes = {node_id: Node(slots={s.name: s.slot_type for s in info.slots}, outcomes=frozenset()) for node_id, info in catalogue.nodes.items()}
        return cls(f"{pack_id}@{version}", _View(pack_id, version, nodes))

    def view(self, pack_ref: str) -> PackView | None:
        return self._view if pack_ref == self.pack_ref else None


def _letters(*parts: str) -> str:
    """26 lower-case letters from the SHA-256 of the parts (each length-prefixed, so ("ab", "c") differs from ("a", "bc")): no digit, so no digit run."""
    digest = hashlib.sha256(b"".join(len(p.encode()).to_bytes(4, "big") + p.encode() for p in parts)).digest()
    number = int.from_bytes(digest, "big")
    out = []
    for _ in range(_ID_LETTERS):
        number, rest = divmod(number, 26)
        out.append(chr(ord("a") + rest))
    return "".join(out)


class DerivedMinter:
    """The identifiers of a proposal (noema#580, open): derived from the request, never from a clock or from randomness.

    `intent_id` is `int_` and 26 letters from the hash of the tenant, the correlation and the client's request id; `version` is 1 and `supersedes` none (a first compile; a compile of an open intent is a
    later change); `created_at` is the canonical input's own `received_at`, the core's clock reading; `raw_ref` is the neutral marker the core overwrites with its own object reference, as it does for the
    identity members (A166; the core's step is noema#565). The candidate set id is `cns_` and 26 letters from a different hash of the same three."""

    def identifiers(self, canonical_input: Mapping[str, Any]) -> Identifiers:
        return Identifiers(intent_id="int_" + _letters("intent", *_key(canonical_input)), version=1, supersedes=None, created_at=str(canonical_input["received_at"]), raw_ref=UNSET)

    def candidate_set_id(self, canonical_input: Mapping[str, Any]) -> str:
        return "cns_" + _letters("candidate-set", *_key(canonical_input))


def _key(canonical_input: Mapping[str, Any]) -> tuple[str, str, str]:
    return str(canonical_input["tenant_id"]), str(canonical_input["correlation_id"]), str(canonical_input["request_id"])


def build_compile_service(environ: Mapping[str, str], harness: Harness | None, *, transport: EvidenceTransport | None = None) -> CompileService:
    """The compile service from the file `NOEMA_MODEL_COMPILE_CONFIG` names. Raises ConfigError for anything missing, unknown, out of range or unsafe: the process then does not start.
    `transport` is for tests; without it the Evidence link is the service's own mutual-TLS transport."""
    require_mtls_listener(environ)
    path = environ.get(COMPILE_CONFIG_ENV)
    if not path:
        raise ConfigError(f"{COMPILE_CONFIG_ENV}: names no file")
    config = require_mapping(_read_json(path, "compile config"), "compile")
    reject_unknown(config, _MEMBERS, "compile")
    missing = sorted(_MEMBERS - set(config))
    if missing:
        raise ConfigError(f"compile: missing {missing}")
    if harness is None:
        raise ConfigError("compile: the harness is not built (NOEMA_MODEL_HARNESS_CONFIG), so there is no provider to ask")
    out_of_scope = require_str(config, "out_of_scope_node", "compile")
    timeout_s = _positive_number(config, "timeout_s")
    max_proposal = require_int(config, "max_proposal_bytes", "compile", minimum=1)
    max_request = require_int(config, "max_request_bytes", "compile", minimum=1)
    pack_file = Path(require_str(config, "pack_file", "compile"))
    pack = require_mapping(_read_json(str(pack_file), "compile.pack_file"), "compile.pack_file")
    packs = PackFileResolver.from_mapping(pack, out_of_scope_node=out_of_scope)
    pipeline = load_pipeline_config(pack, pack_file.parent, out_of_scope_node=out_of_scope)
    lexicon = load_pack_lexicons(pack, Path(require_str(config, "lexicon_dir", "compile")))
    evidence = EvidenceClientConfig.from_mapping(config["evidence"])
    tls = require_mapping(config["evidence_tls"], "compile.evidence_tls")
    reject_unknown(tls, _TLS_MEMBERS, "compile.evidence_tls")
    cert, key, ca = (require_str(tls, name, "compile.evidence_tls") for name in ("cert_file", "key_file", "ca_file"))
    if transport is None:
        transport = UrllibEvidenceTransport(client_ssl_context(cert, key, ca), pool_size=evidence_pool_size(environ))
    compiler = PipelineCompiler({packs.pack_ref: pipeline}, lexicon, harness, EvidenceClient(evidence, transport), DerivedMinter())
    return CompileService(compiler, packs, max_proposal_bytes=max_proposal, timeout_s=timeout_s, max_request_bytes=max_request)
