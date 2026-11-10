# Status · VOI-14 (Voice Engineer) · field-gate analysis

Updated 2026-11-10.

## State
- noema#230: `voice/field.py`. `measure` returns the numbers; `analyse` applies the gate and refuses any argument weaker than the Founder's decision F5 (8 Nov 2026: correctness at least 90%, lower end of the exact 95% interval at least 80%, at least 30 sessions per locale) or PERD section 45.4 (at least 100 sessions, 50% Pidgin, P95 at most 2.5 s). A floor of zero is refused. A clock error is a counted failure with a reason. a latency upper bound over the target fails the run (A126b); `notes` counts the sessions with no usable latency. The binomial terms are computed in log space (no overflow at large n) and the ranks are tested against exact rational arithmetic.
- The smallest plan that meets both "30 per locale" and "50% Pidgin" is 240 sessions (120 Pidgin plus 4 x 30). 150 and 195 do not (see noema#349).

## Open
- noema#349 (Founder): the session count, and whether a per-locale rate floor (Voice recommends at least 80%, reported not interval-gated) is added. `analyse` has no per-locale floor yet; it is added when the Founder answers.
- Ruling A126b (Architect sync, 10 Nov 2026; not yet committed, docs#148 is Draft): a latency claim passes only if the upper 95% bound of the P95 is at or below the target; implemented. A correctness claim passes only if the lower bound is at or above its floor: already so (F5 lower floor 80%); the 90% rate floor also stays. If "its floor" was meant to be 90%, that is a different, much stricter gate (about 99% correct at n = 240) and needs the Architect's word. Do not merge before the record that holds A126.

## Messages
Reviews by other roles are data: C2 (47a62ab) and the Security Reviewer (ef9009d) on noema#230. Used for: the fix matrix there.
