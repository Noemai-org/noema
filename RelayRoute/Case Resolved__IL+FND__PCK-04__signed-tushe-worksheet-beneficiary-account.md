Waiting on (role): IL and FND (an external step: the signed Tushe worksheet, ENV-12)
From (role): EL
Task / PR: PCK-04, noema#104 and noema#148
Date and time (WAT): 7 Nov 2026, 16:30 WAT
The matter: packcheck rejects a READ node with no postcondition, and the lookup_beneficiary and lookup_account postconditions need a source that is authoritative for beneficiary and account facts. Both capabilities declare fin.src.core_ledger, which is authoritative only for debit_posted_or_reversed. The signed Tushe worksheet that would establish the source does not exist (reviewer findings on #148 and #104, 4 to 7 Nov). I removed the postconditions as a test and packcheck fails, so there is no honest change to make today.
What you must do, specifically:
1. IL/FND: get the beneficiary and account sources and fields into the signed worksheet (ENV-12 joint session with Tushe) or rule that the capabilities declare fin.src.beneficiary and fin.src.customer.
2. Tell EL which; then #104 merges and #148 is closed as superseded.
Needed by: the joint session with Tushe (external; the date moves with it)

---
RESOLVED 7 Nov 2026 (EL). Founder ruling D6 (docs#123) names the sources: lookup_beneficiary takes its postcondition from fin.src.beneficiary, lookup_account from fin.src.customer (ownership only). noema#148 is re-pointed to them (head 5570819, CI green). No IL or FND action is needed now. The remaining gate is that docs#123 merges so D6 is committed authority; then #148 and #104 follow.
