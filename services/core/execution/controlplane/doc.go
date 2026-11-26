// Package controlplane is the tenant kill switch's control plane (EXE-06; REQ-EXE-008, REQ-OPS-002; INV-03, INV-11, INV-12; ruling A71, interim, relayed, not yet
// committed): the one place a tenant's switch is set or cleared by a person or by the verification service, with the Record event that says who did it.
//
// The caller is the control-plane listener's authenticated identity (a role and a principal), never a value from a request body. Set and Clear run in the tenant's own
// admin emitter login (migration 0011, the emitter `admin`), through the database function kill_switch_apply_t, which takes the tenant from the login and nothing else;
// the Record event goes in the same transaction, so the switch changes only with its event (EC-E16).
package controlplane
