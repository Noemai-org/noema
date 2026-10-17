// Package db is the Postgres access layer with per-power roles, migrations support and the immutability triggers' helpers (PERD §38). Postgres is the only authoritative store.
//
// May import: platform/{config,telemetry}.
//
// Must never: be imported by the model service, read the temporal database (INV-30), or use a floating-point type for money.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package db
