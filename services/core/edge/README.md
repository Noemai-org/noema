# edge

**What it does.** Terminates client traffic. Built so far (EDG-01): session verification, the identity-in-body guard and the role gate (`edge/session`), and their composition for the public API, `edge.Protected`. Rate limits, request IDs beyond the correlation ID and server-sent events arrive with their tasks.

**REQs.** REQ-API-004, REQ-API-002, REQ-SEC-001, REQ-INP-002, REQ-SEC-008. SoT: INV-12, AGENT-10, AGENT-16, ruling A1.

**How a request is served** (`edge.Protected`, mounted by the composition root under `/v1/` when the session configuration is present):

1. Session check: no credential is 401 `UNAUTHENTICATED`; any bad token is 401 `SESSION_INVALID`, one message for all of them.
2. Role gate of the route: a role the route does not list is 404 `NOT_FOUND_OR_NOT_AUTHORIZED`.
3. Body guard: a body that names an identity at any depth, is not one JSON document, is not `application/json` or exceeds the limit is 400 `INPUT_INVALID`.
4. The route's handler, with the principal in its context.

A path or method no route registers is denied by default: 401 without a session, 404 with one. A configuration that cannot be applied safely (no verifier, a body limit that is not 1 or more and below MaxInt64, a route with no handler or no valid role, a duplicate or a pattern outside `/v1/`) returns an error and no handler. **The public edge serves no control-plane route** (SEC-04, OD-P-79, ruling A54, matrix rows 1 to 7 and 13, rule 7): a pattern whose first segment is `admin` or `benchmark`, or any of whose segments contains `key` or `rotat`, is refused at construction, whatever roles it names. The admin routes belong to the admin listener; key rotation has no route in the POC. The D14 spelling `{v}:publish` cannot be mounted on Go's `ServeMux` at all (a wildcard must be a whole segment); the admin listener's spelling is the Engineering Lead's and the Architect's to decide (noema#505). Each task passes its routes, with their role lists, to `Load` or `Protected` from the composition root (none yet).

**Constructor** (the composition root, `cmd/core/compose.go`, is EL's and is not edited here): `edge.Load(getenv func(string) string, readFile func(string) ([]byte, error), clk clock.Clock, ids session.IDs, routes []edge.Route) (http.Handler, error)`. It returns `nil, nil` when none of the settings is given, an error when some are or any is wrong, and otherwise the protected handler to mount at `/v1/`. `edge.Protected(edge.Options)` is the same chain built from a verifier the caller made.

**Configuration of the core** (all six or none; no defaults, AGENT-16): `NOEMA_SESSION_PUBLIC_KEY_FILE`, `NOEMA_SESSION_KID`, `NOEMA_SESSION_AUDIENCE`, `NOEMA_SESSION_TENANTS` (comma separated, no spaces around the commas: a tenant with surrounding whitespace is refused), `NOEMA_SESSION_LIMITS_FILE` (a `session-limits.v1` document: the clock skew), `NOEMA_MAX_BODY_BYTES`. With none, `Load` gives no handler and the core serves health only; with some, the root must refuse to start. `make bench-tokens` writes a SYNTHETIC set for a benchmark run, and, given `BENCH_PRINCIPALS` and `BENCH_TOKEN_TTL`, one token file per principal (see `deploy/compose/bench_tokens.sh`).

**Not done here.** `session.CheckTenant` (the token's tenant must equal the tenant of the database login, ruling A1 items 5 and 7) is called where a handler reaches the database, because the login tenant comes from the database mapping; the first route that does carries the call and its test.

**Tests.** `go test ./edge/...` from `services/core/`; `make bench-tokens-test` for the token script. `edgetest` makes SYNTHETIC tokens for tests and is imported by no production package.

**Correlation ID (noema#458).** The edge mints one correlation ID per request; the session check puts it in the request context (`session.CorrelationFromContext`) and every later layer, the answer and the Record's chain use that one. A caller's `Noema-Correlation-Id` is never adopted or echoed, because a value such as `corr_0123456789` has the shape of an account number (REQ-SEC-003, AGENT-13).
