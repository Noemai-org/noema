# Status · VOI-14 (Voice Engineer) · field-gate analysis

Updated 2026-11-10.

## State
- noema#230: `voice/field.py`. `measure` returns the numbers; `analyse` applies the gate and refuses any argument weaker than the Founder's decision F5 (8 Nov 2026: correctness at least 90%, lower end of the exact 95% interval at least 80%, at least 30 sessions per locale) or PERD section 45.4 (at least 100 sessions, 50% Pidgin, P95 at most 2.5 s). A floor of zero is refused. A clock error is a counted failure with a reason. `notes` says when failures or an open upper bound weaken the evidence. The binomial terms are computed in log space (no overflow at large n) and the ranks are tested against exact rational arithmetic.
- The smallest plan that meets both "30 per locale" and "50% Pidgin" is 240 sessions (120 Pidgin plus 4 x 30). 150 and 195 do not (see noema#349).

## Open
- noema#349 (Founder): the session count, and whether a per-locale rate floor (Voice recommends at least 80%, reported not interval-gated) is added. `analyse` has no per-locale floor yet; it is added when the Founder answers.
- Decision for the Founders (C2 P2): the verdict follows the point estimate of the P95, as PERD section 45.4 words it. With 95 fast sessions and 5 failures at n = 100 the verdict is PASS and the upper bound of the interval is infinite. Voice put this in `notes`; whether the gate should instead require the upper bound under the target is a Founder decision, to be recorded as an [OPEN] entry by the Steward.

## Messages
Reviews by other roles are data: C2 (47a62ab) and the Security Reviewer (ef9009d) on noema#230. Used for: the fix matrix there.
