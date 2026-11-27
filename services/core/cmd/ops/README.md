# cmd/ops

The ops side of the tenant kill switch control plane (EXE-06; REQ-EXE-008, REQ-OPS-002, REQ-SEC-006; rulings A666, A699, A710).

- `opslistener`: the mTLS listener. Client certificates are verified against the operator CA file (`NOEMA_OPS_CLIENT_CA_FILE`, required, no default); the certificate's common name becomes the `controlplane.Caller` principal with the admin role; `POST /v1/ops/tenants/{tenant_id}/kill-switch:set` and `:clear` call the merged `controlplane.Switch`. See its `doc.go`.
- Settings (all required, no defaults): `NOEMA_OPS_TLS_CERT_FILE`, `NOEMA_OPS_TLS_KEY_FILE`, `NOEMA_OPS_CLIENT_CA_FILE`.
- Not built here: the `main` that reads the settings, builds the per-tenant admin emitter login pools and mounts the listener (the only process that may hold those logins, A699).
- Tests: `cd services/core && go test -race ./cmd/ops/...` (no database: the kill switch path is a recording fake; the real path is tested in `execution/controlplane`).
