# services/core

The Go core service (Python is the other deployable: `services/model`). It hosts Trust, Execution, Outcome, the Record, the Evidence Layer, the enterprise adapters and the Temporal workers (SoT §35.6, PERD §64.2).

**Status (PLT-01).** The boundaries exist as empty, buildable packages; each `doc.go` states the package's responsibility, what it may import and what it must never do. The only running code is `cmd/core`, which serves `/healthz` (200) and `/readyz` (503 until dependencies exist).

- **Module:** `github.com/Noemai-org/noema/services/core`; the `toolchain` line in `go.mod` pins Go.
- **Build context:** this folder only (DEC-2026-10-13 R2): `docker build services/core`.
- **Tests:** `go test ./...` from this folder, or `make test-core` from the repository root.
- **Rules that CI enforces on these packages** arrive with PLT-03 (`tools/lint/`).
