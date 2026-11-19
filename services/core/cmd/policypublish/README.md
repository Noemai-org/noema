# cmd/policypublish

**What it does.** Publishes the policy version of a pack for one tenant, so that the Trust intake finds a policy in force. `policypublish` reads the pack's `policy` document (`packs/fintech_v1.json`, key `policy`: `fin.pol`, version `3.1`), checks it with the Trust policy loader (`trust/policy.Parse`) against the vocabulary the same pack declares (its capabilities, evidence descriptors and predicate variables), and calls `policystore.Publish` as the tenant's Trust login, signed as `core-trust`, in one transaction with the `POLICY_PUBLISHED` Record event. The policy ID and the version are the document's own: no flag sets them.

**REQs.** REQ-TRU-005, REQ-OPS-003, REQ-AUD-001; INV-09, INV-12. Task TRU-02. Updated 19 Nov 2026.

**Usage.**

```
policypublish --tenant ten_aa --pack-dir packs --pack-file fintech_v1.json \
  --dsn-file /run/secrets/trust_dsn_ten_aa --key-dir /run/secrets/core-trust-keys --key-max-active 1 --published-by adm_ops01
```

| Flag | Meaning |
|---|---|
| `--tenant` | the tenant ID (`ten_` and 2 to 32 letters or digits) |
| `--pack-dir`, `--pack-file` | the pack file, read inside the directory only: a name that leaves it, by `..` or by a link, is refused |
| `--dsn-file` | a regular file (not a link) holding the DSN of the tenant's `trust_rw__<tenant>` login. The DSN is never an argument and never printed |
| `--key-dir`, `--key-max-active` | the directory of the `core-trust` signing keys (`core-trust.ed25519.<version>.key`, mode 0400, `execution/signing.LoadRing`), and how many may be mounted at once: the rotation overlap, with no default. The newest key signs; its public key must be registered with `key_register` |
| `--published-by` | the administrator's principal ID, for example `adm_ops01`; it goes on the Record event as `admin:<id>`. No default |

**Output.** One JSON line on stdout with the PERD 44.2 fields (`ts`, `level`, `service`, `power`, `event`, `correlation_id`, `tenant_id`, `msg`) and `result` (`published`, `already_published`, `refused`, `error`), `reason`, `policy` (`fin.pol@3.1`) and `sqlstate` when there is one. When the exit code is not 0, one fixed sentence on stderr. Never a DSN, a login name, a key or a driver's text.

| Exit | Meaning |
|---|---|
| 0 | published, or the version was already the active one (nothing written) |
| 1 | refused: `login_not_for_tenant` (the login is not the tenant's Trust login; the same answer for a tenant that does not exist), `not_newer` (an older version than the active one), `invalid_policy` (the document or its vocabulary is refused), `invalid_request` (a version, tenant, policy ID or publisher outside the publication store's pattern) |
| 2 | could not run: a bad argument, an unreadable pack, DSN file or key directory, a database that cannot be reached, a Record append that failed (nothing is kept, the publication is in the same transaction) |

**Idempotent.** A second run with the pack's version already active says `already_published` and writes nothing. Two runs at once give one publication: the database serialises the version check, and the run that loses reads the active version again and reports `already_published`.

**What it does not do.** It cannot tell two different documents of one version apart: the publication record holds `(tenant, policy_id, version)`, not the document, and the document store is DEBT-23 (open). A changed rule set under an unchanged version is therefore reported as `already_published`; change the version in the pack. It creates no tenant, no login, no mapping and no signing key (`deploy/compose/postgres/` does), and it adds no Record event of its own: `policystore.Publish` writes `POLICY_PUBLISHED`.

**Tests.** `go test ./cmd/policypublish/...` from `services/core/`, against a real PostgreSQL (`internal/testpg`).
