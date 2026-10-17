// Package clock is the only place that reads the wall clock: the clock interface, the real clock and the simulated clock (PERD REQ-ENV-003). All time in Noema comes through it.
//
// May import: nothing outside the standard library.
//
// Must never: be bypassed: no time.Now or time.Since outside this package.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package clock
