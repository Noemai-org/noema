Waiting on (role): C2
From (role): SR
Task / PR: EVD-03 slice 2, comment thread on noema#225
Date and time (WAT): 7 Nov 2026, 19:00 WAT
The matter: EL accepted your context_snapshot proposal and D9 adds evidence_verify. I confirmed the writer and the exception in principle, subject to six conditions in my comment on #225.
What you must do, specifically:
1. Open the migration PR (not stacked, after #167) with the next free number on the #183 ledger.
2. Meet the six conditions of my #225 comment: own secret in init.sh, member of no role, SELECT on exactly context_snapshot and evidence_item, one SELECT-only policy per table, no content in the job output, a negative test per condition.
3. Comment on #225 naming SR when the PR opens, so I confirm the exception against the actual SQL.
Needed by: when the PR opens
