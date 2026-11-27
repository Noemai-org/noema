// Package rowreader is the row reader behind explanation.FactsSource (EXP-01; A550, A556, A665, A675 and A710, the Architect's order of 26 Nov 2026, interim, relayed): it reads
// the stored rows an explanation needs, as the tenant's own Trust login, and turns them, with the facts of the payment seam, into the explanation-facts.v2 the Explainer reads.
//
// Reader reads one decision's rows in a single read-only transaction: the Decision Record, the sealed snapshot's classification, the admitted intent's subject, and the grants,
// receipts and verdicts of the decision's investigation. Every statement names the tenant, and the login's row-level security binds it again. Source builds the facts: a case
// reference only from an ACKNOWLEDGED receipt with its acknowledgement and an ACHIEVED verdict (INV-04, INV-07), the outcome from the verdict's own definition, the classification
// from the sealed snapshot, and everything else an unknown; the payment facts and the locale come from seams the composition fills.
//
// May import: the standard library, the database driver, explanation, explanation/factsource, internal/contracts, platform/clock.
//
// Must never: write a row; read the Record, the evidence payloads or any table of another power; hold a vocabulary of the pack (a capability, an outcome, a queue or a slot name is
// configuration); name a counterparty; default a locale, a zone, a currency or a label; show a value from a row that is absent, ambiguous or of another tenant, decision or
// correlation (INV-04, INV-07, INV-12, INV-14, INV-19, AGENT-13, AGENT-16, AGENT-17).
package rowreader
