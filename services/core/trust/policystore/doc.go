// Package policystore: the policy versions a tenant has published and which one is active (TRU-08 slice 1).
//
// Implements: REQ-TRU-005 and REQ-OPS-003 in part (the publication record, which keeps the hash of the policy document, and the active-version read). The grant is not voided at publication: E4 voids
// it at execution (Founder decision D8, 7 Nov 2026), and only execution_rw writes decision_grant.status. Not built here: the composite production binder,
// the control-plane endpoint and the policy document store (DEBT-23, still open).
//
// May import: internal/contracts, record/chain, platform/{clock,ids}.
//
// Must never: import execution, outcome or their internals (Execution reads the active version through a small adapter, not through this package's
// types); compare versions across policy IDs; decide a version's order itself. The database decides it (a trigger on policy_publication), so a caller
// cannot make an older policy active again.
//
// The publication row keeps document_hash, the hash of the policy document (policy.DocumentHash; migration 0035, ruling on noema#771, interim). Publish requires it and the database
// requires it for every new publication; a publication made before the migration keeps a NULL hash. This package does not compute the hash and does not import trust/policy: the caller
// passes it. The POLICY_PUBLISHED event carries the same hash as document_hash, written in the same transaction (ruling on noema#771, INTERIM: relayed by the Founder in session,
// not yet committed, AGENTS section 2.3). The documents themselves are not stored (DEBT-23).
//
// The publisher is the authenticated database role (ruling A474, INV-12): Request has no publisher member, the INSERT fills published_by from session_user in the statement itself, and RETURNING
// gives the same value to the event's actor (admin:<role>), so the row and the event cannot differ. The role is not in the payload: the Record's payload guard (A184) refuses a string with a run of
// more than four digits, and a login's name can hold one. A login whose name is no valid actor form cannot publish: the append refuses and the transaction keeps nothing.
//
// The active version of a policy_id is the highest one published for it, returned as policy_id@version, the form a Decision Grant binds
// (decision-grant.v1 policy_version). A policy with no publication has no active version and the result is "".
//
// Published lists every version of a policy a tenant has published, oldest first, with the database's publication time in whole Unix seconds. The Trust intake's Policies
// adapter reads it to find the version in force at a time (an AT_ADMISSION rule is read from the version in force when the intent was admitted, INV-09).
package policystore
