"""Mutual TLS for the model service end of the svc_net link to the core.

Implements: REQ-SEC-002, REQ-API-005, INV-29.

May import: the standard library and declared dependencies.

Must never: hold a core-database, Temporal or grant credential; fall back to a shared bearer token.

`client_ssl_context` builds the client side: TLS 1.2 or later, the server verified against the given CA with its hostname
checked, and a client certificate presented. The files are mounted secrets read at start-up (AGENTS.md section 10).
"""

import ssl

from noema_model.platform.config import ConfigError


def client_ssl_context(cert_file: str, key_file: str, ca_file: str) -> ssl.SSLContext:
    try:
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        context.minimum_version = ssl.TLSVersion.TLSv1_2
        context.verify_mode = ssl.CERT_REQUIRED
        context.check_hostname = True
        context.load_verify_locations(cafile=ca_file)
        context.load_cert_chain(certfile=cert_file, keyfile=key_file)
    except (OSError, ssl.SSLError) as exc:
        raise ConfigError("mTLS material is missing or unusable") from exc
    return context
