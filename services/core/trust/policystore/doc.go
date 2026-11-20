// Package policystore: the policy versions a tenant has published and which one is active (TRU-08 slice 1).
//
// Implements: REQ-TRU-005 and REQ-OPS-003 in part (the publication record and the active-version read). The grant is not voided at publication: E4 voids
// it at execution (Founder decision D8, 7 Nov 2026), and only execution_rw writes decision_grant.status. Not built here: the composite production binder,
// the control-plane endpoint and the policy document store (DEBT-23, still open).
//
// May import: internal/contracts, record/chain, platform/{clock,ids}.
//
// Must never: import execution, outcome or their internals (Execution reads the active version through a small adapter, not through this package's
// types); compare versions across policy IDs; decide a version's order itself. The database decides it (a trigger on policy_publication), so a caller
// cannot make an older policy active again.
//
// The active version of a policy_id is the highest one published for it, returned as policy_id@version, the form a Decision Grant binds
// (decision-grant.v1 policy_version). A policy with no publication has no active version and the result is "".
//
// Published lists every version of a policy a tenant has published, oldest first, with the database's publication time in whole Unix seconds. The Trust intake's Policies
// adapter reads it to find the version in force at a time (an AT_ADMISSION rule is read from the version in force when the intent was admitted, INV-09).
package policystore
