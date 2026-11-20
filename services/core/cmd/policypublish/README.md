# cmd/policypublish

**What it does.** Publishes the policy version of a pack for one tenant, so that the Trust intake finds a policy in force. `policypublish` reads the pack's `policy` document (`packs/fintech_v1.json`, key `policy`: `fin.pol`, version `3.1`), checks it with the Trust policy loader (`trust/policy.Parse`) against the vocabulary the same pack declares (its capabilities, evidence descriptors and predicate variables), computes its document hash (`trust/policy.DocumentHash`), and calls `policystore.Publish` as the tenant's Trust login, signed as `core-trust`, in one transaction with the `POLICY_PUBLISHED` Record event. The policy ID and the version are the document's own: no flag sets them.

**REQs.** REQ-TRU-005, REQ-TRU-004, REQ-OPS-003, REQ-AUD-001; INV-09, INV-12, INV-21. Task TRU-02. Updated 20 Nov 2026 (the document hash: ruling on noema#771, interim; migration 0035; the hash in the event payload, interim).

**The document hash.** The publication row keeps `document_hash`: `sha256:` and 64 hex digits, the SHA-256 of the domain tag `noema/policy-document/v1`, a 0x00 byte and the RFC 8785 form of the pack's `policy` member. It is a hash of the document, not of its spelling: key order and whitespace do not change it. The command prints the hash and never the document. The `POLICY_PUBLISHED` event on the Record carries the same value as `document_hash` in its payload, written in the same transaction as the row (INTERIM, as above). The documents themselves are still not stored (DEBT-23); a loader that reads the pack again recomputes the hash with `policy.DocumentHash` and compares it with the published one.

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

**Output.** One JSON line on stdout with the PERD 44.2 fields (`ts`, `level`, `service`, `power`, `event`, `correlation_id`, `tenant_id`, `msg`) and `result` (`published`, `already_published`, `refused`, `error`), `reason`, `policy` (`fin.pol@3.1`), `document_hash` (the hash of the document offered, once it is read) and `sqlstate` when there is one. When the exit code is not 0, one fixed sentence on stderr. Never a DSN, a login name, a key or a driver's text.

| Exit | Meaning |
|---|---|
| 0 | published, or the version was already the active one (nothing written) |
| 1 | refused: `login_not_for_tenant` (the login is not the tenant's Trust login; the same answer for a tenant that does not exist), `not_newer` (an older version than the active one), `invalid_policy` (the document or its vocabulary is refused), `invalid_request` (a version, tenant, policy ID, publisher or document hash outside the publication store's pattern), `document_changed` (the version is the active one but the publication holds the hash of another document: publish a new version), `document_unknown` (the version is the active one but its publication has no hash because it was made before migration 0035: publish a new version) |
| 2 | could not run: a bad argument, an unreadable pack, DSN file or key directory, a database that cannot be reached, a Record append that failed (nothing is kept, the publication is in the same transaction) |

**Idempotent.** A second run with the pack's version already active **and the same document** says `already_published` and writes nothing, whatever the document's spelling. Two runs at once give one publication: the database serialises the version check, and the run that loses reads the stored hash again and reports `already_published` when it is the same document. A different document under the active version is `document_changed` (exit 1); an active version published before the hash existed is `document_unknown` (exit 1, the safe reading: there is nothing to compare with, so the command neither claims "already published" nor writes). Both are cleared the same way: publish a new version.

**What it does not do.** It does not store the document (DEBT-23). It creates no tenant, no login, no mapping and no signing key (`deploy/compose/postgres/` does), and it adds no Record event of its own: `policystore.Publish` writes `POLICY_PUBLISHED`, and that event's payload carries the hash (`document_hash`, the value the row stores; **INTERIM**: a ruling on noema#771 relayed by the Founder in session, not yet committed, AGENTS section 2.3; the Steward is to record it).

**Tests.** `go test ./cmd/policypublish/...` from `services/core/`, against a real PostgreSQL (`internal/testpg`).
