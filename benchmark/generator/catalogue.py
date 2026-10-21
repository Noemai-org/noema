"""The identifiers the coverage rule is checked against (PERD §48.1 "Coverage rule"; REQ-BEN-001).

A vendored extract of Noemai-org/docs, docs/poc/perd/NOEMA_POC_PERD_v1.11.md at docs commit
1156558158a920f46f93ad2d3b084a06dd251eef: the 22 user stories (§11.2), the 8 supporting stories (§11.2), the 105 edge cases
(§12) and the 28 fault effects (§37.1). The PERD is normative; this copy only lets the build fail on a gap without reading the
docs repository. The capabilities come from the pack at generation time, never from here.
"""

USER_STORIES = tuple(f"US-{n:02d}" for n in range(1, 23))
SUPPORTING_STORIES = tuple(f"PUS-{n:02d}" for n in range(1, 9))

EDGE_CASES = (
    *(f"EC-A{n:02d}" for n in range(1, 21)),
    *(f"EC-S{n:02d}" for n in range(1, 23)),
    *(f"EC-T{n:02d}" for n in range(1, 12)),
    *(f"EC-E{n:02d}" for n in range(1, 17)),
    *(f"EC-U{n:02d}" for n in range(1, 6)),
    *(f"EC-X{n:02d}" for n in range(1, 20)),
    *(f"EC-C{n:02d}" for n in range(1, 4)),
    *(f"EC-V{n:02d}" for n in range(1, 6)),
    *(f"EC-K{n:02d}" for n in range(1, 5)),
)

FAULT_EFFECTS = (
    "LATENCY", "TIMEOUT_BEFORE_COMMIT", "TIMEOUT_AFTER_COMMIT", "NEAR_TIMEOUT_BEFORE", "NEAR_TIMEOUT_AFTER",
    "DROP_RESPONSE", "HTTP_200_SEMANTIC_PENDING", "HTTP_5XX", "HTTP_429", "MALFORMED_RESPONSE",
    "DROP_WEBHOOK", "DUPLICATE_WEBHOOK", "REORDER_EVENTS", "LATE_EVENT",
    "STALE_READ", "PAGINATE", "SCHEMA_DRIFT", "CLOCK_SKEW",
    "SERVICE_UNAVAILABLE",
    "SPLIT_STATE", "DEBIT_WITHOUT_CREDIT", "DELAYED_SETTLEMENT", "DELAYED_REVERSAL", "IMPOSSIBLE_STATE",
    "CONCURRENT_MODIFICATION", "POLICY_MUTATION", "IDEMPOTENCY_COLLISION",
    "INJECTION_PAYLOAD",
)

# PERD §48.1 category table: cases per category, in table order. The release suite has at least 320 deterministic cases.
CATEGORY_QUOTAS = (
    ("CLEAN_UNIQUE", 40), ("AMBIGUITY", 50), ("STATE_CONFLICT", 60), ("TIMEOUT_UNKNOWN", 35), ("DUPLICATE_REPLAY", 30),
    ("AUTHORISATION", 30), ("POLICY_CLOCK", 20), ("PROMPT_INJECTION", 20), ("STALE_SESSION", 15), ("INTEGRATION_HOSTILITY", 20),
)
CATEGORY_CODES = {
    "CLEAN_UNIQUE": "CLEAN", "AMBIGUITY": "AMB", "STATE_CONFLICT": "STATE", "TIMEOUT_UNKNOWN": "TMO", "DUPLICATE_REPLAY": "DUP",
    "AUTHORISATION": "AUTH", "POLICY_CLOCK": "POL", "PROMPT_INJECTION": "INJ", "STALE_SESSION": "SESS", "INTEGRATION_HOSTILITY": "HOST",
}
MIN_PIDGIN_OR_CODE_SWITCHED_PERCENT = 30  # PERD §48.1: at least 30% in Pidgin or code-switched
