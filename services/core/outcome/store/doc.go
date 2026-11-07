// Package store is Outcome's Postgres adapter: it loads the receipt a verification starts from and writes a verdict,
// its RecoveryRequest, its watch and their Record events in one transaction, as the outcome_rw role (OUT-01;
// REQ-VER-001, REQ-VER-003, REQ-VER-004, REQ-AUD-001).
//
// May import: outcome/verifier (its types), record/chain, platform/{clock,crypto,db,ids}, the database driver.
//
// Must never: import trust, execution or evidence; write any table directly (the database function
// outcome_record_verdict is the only write path); hold a credential other than outcome_rw's.
package store
