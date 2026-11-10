# Status · VOI-14 (Voice Engineer) · field-gate analysis

Updated 2026-11-10.

## State
- noema#230: `voice/field.py`. `measure` returns the numbers; `analyse` applies the gate and refuses any argument weaker than the Founder's decision F5 (8 Nov 2026: correctness at least 90%, lower end of the exact 95% interval at least 80%, at least 30 sessions per locale) or PERD section 45.4 (at least 100 sessions, 50% Pidgin, P95 at most 2.5 s). A floor of zero is refused. A clock error is a counted failure with a reason. a latency upper bound over the target fails the run (A126b); `notes` counts the sessions with no usable latency. The binomial terms are computed in log space (no overflow at large n) and the ranks are tested against exact rational arithmetic.
- The smallest plan that meets both "30 per locale" and "50% Pidgin" is 240 sessions (120 Pidgin plus 4 x 30). 150 and 195 do not (see noema#349).

## Open
- Founder decision on noema#349 (10 Nov 2026), implemented: 240 field sessions (120 Pidgin plus 30 in each of en-NG, Yoruba, Hausa and Igbo); the pooled gate (point rate at least 90% AND the lower 95% bound at least 80%, A96); each locale's own rate at least 80% (24 of 30), reported per locale (`locale_correct_rates`); a latency claim passes only if the upper 95% bound of the P95 is at or below the target. `analyse` refuses any weaker argument (named constants `FIELD_MIN_SESSIONS`, `F5_MIN_LOCALE_CORRECTNESS`, and the earlier ones).
- Ruling A126b (Architect sync, 10 Nov): the latency rule is the same as the Founder's. Do not merge before the record that holds A126 if it is still uncommitted.
- Not in this PR: the field plan documents (noema#201 recording plan, #219 protocol) still say 150 sessions or the old numbers; Voice aligns them in a separate docs commit on each PR.

## Messages
Reviews by other roles are data: C2 (47a62ab) and the Security Reviewer (ef9009d) on noema#230. Used for: the fix matrix there.
