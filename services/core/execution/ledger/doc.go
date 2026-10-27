// Package ledger is the Postgres implementation of the controller's Ledger and GrantSource ports: the one place
// that writes the Execution tables and appends Execution's Record events (PERD 27.1 steps E8 and E11, 32.4).
//
// Implements: REQ-EXE-001 (single use by unique constraint), REQ-EXE-002 (the key is committed before dispatch),
// REQ-EXE-006 (the sweeper's input), REQ-AUD-001 (events appended in the transaction of the object write),
// REQ-DATA-001 (runs as the execution_rw role). Task EXE-02 (production implementation of its ports).
//
// May import: execution/controller, platform/{clock,crypto,db}, record/chain.
//
// Must never: dispatch; call an adapter; read or write a table Execution does not own; retry a spend; write the
// Record any other way than inside the transaction of the object write; decide anything.
package ledger
