# tools/lint

Architecture lint (PLT-03; SoT §51, REQ-OPS-003). Changes under this path need both founders (CODEOWNERS).

Run: `make arch-lint` (or `python3 -I tools/lint/run.py`). Prove the rules can fail: `python3 -I tools/lint/run.py --canaries`.

Go rules run in `golint/` with `go/parser` and `go/ast` (standard library, no module download). Python rules run in `pyrules.py` with `ast` and `tomllib`.

| Rule id | Fails when |
|---|---|
| `import-boundary` | `trust`, `execution` or `outcome` reaches another of the three, directly or through other packages. Traversal stops at `internal/contracts`. |
| `import-model-client` | `trust`, `execution`, `outcome` or `record` reaches an explanation or model client (path segment `explanation`, `modelclient`, `providers`, `voice`, `understanding`, or `services/model`) |
| `import-adapters` | a package other than `execution`, `evidence` or `adapters` imports `adapters` |
| `clock` | `time.Now`, `time.Since` or `time.Until` outside `platform/clock` |
| `py-clock` | `datetime.now`, `utcnow` or `today` outside the model service's `platform/clock` |
| `money-float-area` | `float32`, `float64` or a float literal in `trust`, `execution`, `outcome` or `record` |
| `money-float-field` | a float on a field or value named `amount*` or `*minor`, or on `Quantity.value`, anywhere in the core |
| `py-money-field` | `float` or `Decimal` on the same names in the model service |
| `money-literal-multiplier` | a literal power of ten (10, 100, 1000, 1e2, `math.Pow10`) multiplies, divides or takes the remainder of a value named `amount*` or `*minor` anywhere in the core; the exponent comes from `contracts/currencies.json` (M1) |
| `money-decimal-field` | a field or value named `amount*` or `*minor` has a `Decimal`, `decimal.Decimal`, `big.Float` or `big.Rat` type in the core (M4) |
| `py-money-multiplier` | a literal power of ten (10, 100, `1e2`, `10 ** n`, `pow(10, n)`) multiplies, divides or scales a name or attribute `amount*` or `*minor` in the model service (M1) |
| `money-sql-type` | in `services/core/migrations`, a column or cast named `amount*` or `*minor` is `NUMERIC`, `DECIMAL`, `REAL`, `DOUBLE PRECISION` or `FLOAT`, or any column is `MONEY` (M4). Comments and string literals are not read |
| `forbidden-term` | naira, NIBSS, NIP, BVN, NUBAN, kobo, CBN, ledger_balance, ICD, HIPAA or MSISDN in a core identifier or string literal |
| `manifest-provider-sdk` | a provider SDK in `services/core/go.mod` |
| `manifest-model-db` | a DB driver or Temporal client in the model manifest or lock |
| `mock-library` | a mock library imported in `benchmark/` or any `integration/` package (Go or Python) |
| `benchmark-branch` | a benchmark-aware identifier, string or build tag in the core (`benchmark`, `heldout`, `oracle`, `bench_mode`) |
| `cross-service` | the core references the model service |
| `py-cross-service` | the model service references the core |
| `dockerfile-cross-service` | a service Dockerfile reaches outside its own context |
| `threshold-literal` | a number other than 0 or 1 in non-test code of `trust`, `execution` or `outcome`; thresholds come from `platform/config` |
| `verifier-independence` | `outcome/verifier` reaches `trust/` or `evidence/`, directly or transitively; the verifier uses its own reads (DEC-2026-10-20) |
| `predicate-purity` | `internal/predicate` imports `trust`, `execution`, `outcome` or `record`, an I/O, clock, network, database or random package, or calls `fmt.Print*` |
| `candidates-api-shape` | a `*Response` or `*Candidate` type in `evidence/candidates_api` is not a struct, embeds a type, or has a field outside the allowed candidate fields (no free text) |
| `construct-decision-grant` | `DecisionRecord` is built outside `trust/decide`, or `DecisionGrant` outside `trust/grants` (generated `internal/contracts` and tests exempt) |
| `harness-only-providers` | the model service imports `providers` anywhere except `harness/` (and `providers/` itself); `voice/` may import `providers.speech` and nothing else in `providers` (A9) |
| `pipeline-purity` | `understanding/pipeline` has module-level mutable state, `global`, or imports a database, Temporal, HTTP or agent-framework client |
| `crash-hook` | a `Boundary` field (the crash-point matrix's hook in `execution/ledger`, `execution/controller` and `outcome/verifier`) is set to anything but `nil` by an assignment (also in parentheses), a pointer to the field (`&x.Boundary`), a composite-literal key, or a positional composite literal of a struct that declares it (also an elided element of a slice or map), in a file that is not `_test.go`; declaring, reading, calling, comparing and clearing it are fine (noema#553, noema#628) |

## Decisions

- **Comments are not scanned for forbidden terms.** Documentation may name what the core must not contain. The rule reads identifiers and string literals from the syntax tree, which never includes comments. Terms match whole words after splitting identifiers (`NIPCode` is `nip`, `code`; `unipolar` does not match).
- **Imports are checked by syntax, transitively.** The graph of in-module imports is built from the parsed sources, so the lint also works on a canary tree that is not a buildable module.
- **Money rules.** `money-float-area` is stricter than `money-float-field`: the four deciding areas allow no float at all.
- **Money rules are by name, not by type alone** (CON-05). A `Decimal` or `big.Rat` is legitimate for a rate or for the lexicon's spoken-amount conversion (M5), so the rules read the name of the value (`amount*`, `*minor`), as `money-float-field` does. A money value is never a decimal, a float or a literal-scaled number (M1, M4, M6).
- **`threshold-literal`** allows only 0 and 1 in the three decision areas, so a threshold, TTL, margin or budget cannot be written inline (REQ-OPS-003).

## Canaries

`canaries/<rule>/` (or `<rule>__<variant>/`) is a miniature tree laid out like the repository. The harness lints each one and requires that exactly that rule fires. A tree named `clean__*` is a positive control: allowed patterns that no rule may flag. `test_money_rules.py` covers the edges of the money rules (`python3 -I -m unittest discover -s tools/lint -p 'test_*.py'`). The real lint never scans `tools/lint/canaries`; the exclusion is by path in the walker, and no rule is weakened.
