// Package record is the independent, append-only, hash-chained witness of everything, with reconstruction, operational memory and chain verification (SoT §14; PERD §32). It can do nothing.
//
// May import: platform/{db,crypto,clock,config,telemetry,redaction}, internal/contracts.
//
// Must never: act, be treated as operational truth, import another power's internals or any model or provider client, let a model write into memory as an instruction, or use a float type.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package record
