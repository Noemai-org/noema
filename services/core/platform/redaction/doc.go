// Package redaction is the mandatory redactor: masking to the last four digits and removal of secrets before any log, trace or provider call (PERD §44.2).
//
// May import: the standard library.
//
// Must never: let any log line, trace or model-provider request bypass it.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package redaction
