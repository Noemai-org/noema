# cmd/ops

The ops side of the tenant kill switch control plane (EXE-06; REQ-EXE-008, REQ-OPS-002, REQ-SEC-006; rulings A666, A699, A710).

- `opslistener`: the mTLS listener. Client certificates are verified against the operator CA file (`NOEMA_OPS_CLIENT_CA_FILE`, required, no default); the certificate's common name becomes the `controlplane.Caller` principal with the admin role; `POST /v1/ops/tenants/{tenant_id}/kill-switch:set` and `:clear` call the merged `controlplane.Switch`. See its `doc.go`.
- Settings (all required, no defaults): `NOEMA_OPS_TLS_CERT_FILE`, `NOEMA_OPS_TLS_KEY_FILE`, `NOEMA_OPS_CLIENT_CA_FILE`.
- Not built here: the `main` that reads the settings, builds the per-tenant admin emitter login pools and mounts the listener (the only process that may hold those logins, A699).
- Tests: `cd services/core && go test -race ./cmd/ops/...` (no database: the kill switch path is a recording fake; the real path is tested in `execution/controlplane`).

## The process (A775)

- `main.go` is the `cmd/ops` process: the only one that holds the tenants' admin emitter logins (A699). It binds `NOEMA_OPS_LISTEN_ADDR` (host:port) and serves the two kill switch routes over mutual TLS.
- Required settings, no defaults: `NOEMA_OPS_LISTEN_ADDR`, `NOEMA_OPS_DSN_DIR` (one 0400 file per tenant, named by the tenant ID), `NOEMA_OPS_KEY_DIR` and `NOEMA_OPS_KEY_MAX_ACTIVE` (admin signing keys `admin.ed25519.<n>.key`), `NOEMA_OPS_TLS_CERT_FILE`, `NOEMA_OPS_TLS_KEY_FILE`, `NOEMA_OPS_CLIENT_CA_FILE`, `NOEMA_OPS_SWITCH_TIMEOUT_MS`, `NOEMA_OPS_HEADER_TIMEOUT_MS`, `NOEMA_OPS_READ_TIMEOUT_MS`, `NOEMA_OPS_IDLE_TIMEOUT_MS`, `NOEMA_OPS_MAX_HEADER_BYTES`.
- Revocation: there is none; the operator CA must issue short-lived client certificates (see `opslistener/doc.go`).
- Tests: `cd services/core && go test -race ./cmd/ops/...`
