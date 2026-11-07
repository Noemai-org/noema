// Package policystore: the policy versions a tenant has published and which one is active (TRU-08 slice 1).
//
// Implements: REQ-TRU-005 and REQ-OPS-003 in part (the publication record and the active-version read). Not built here: the publication transaction
// that voids every unspent grant at once (the lazy void at E4 holds the guarantee until then, DEBT-23), the control-plane endpoint and the policy
// document store.
//
// May import: internal/contracts, record/chain, platform/{clock,ids}.
//
// Must never: import execution, outcome or their internals (Execution reads the active version through a small adapter, not through this package's
// types); compare versions across policy IDs; decide a version's order itself. The database decides it (a trigger on policy_publication), so a caller
// cannot make an older policy active again.
//
// The active version of a policy_id is the highest one published for it, returned as policy_id@version, the form a Decision Grant binds
// (decision-grant.v1 policy_version). A policy with no publication has no active version and the result is "".
package policystore
