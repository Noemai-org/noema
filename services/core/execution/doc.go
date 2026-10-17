// Package execution carries out exactly what a Decision Grant allows, once: the planner, the capability registry, the execution controller and recovery (SoT §13; PERD §24 to §27). It has no discretion and cannot judge success.
//
// May import: platform/*, internal/contracts, adapters and evidence.
//
// Must never: import trust or outcome internals, any model or provider client, or an adapter for a PROHIBITED capability (there is none, BD-08); retry without a new grant (AGENT-08), choose a capability, target or parameter beyond the grant, declare success, or use a float type.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package execution
