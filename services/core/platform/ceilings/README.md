# platform/ceilings

The two hard upper bounds on what Trust shows a principal in a clarification question: the most options (4) and the longest digit run in an option label (4). A pack may be stricter and never looser; a looser pack value is refused by the step that reads it, not clamped (noema#571, Security Reviewer's blocking finding; A154 as corrected; A205 Q1).

- **REQ and SoT:** REQ-AMB-004, REQ-SEC-003, AGENT-16. Sources: PERD v1.24 lines 1810, 1891, 1892, 6063 (options and candidates); `masking-limits.v1` (digits).
- **Why in code:** the lint forbids a numeric literal in code under `trust/`, and a pack value is data. The bound has to stand outside both.
- **Tests:** `go test ./platform/ceilings/` (the digit ceiling equals the pack's masking limit).
