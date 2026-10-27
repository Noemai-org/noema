// Package db is the Postgres access layer with per-power roles, migrations support and the immutability triggers' helpers (PERD §38). Postgres is the only authoritative store.
//
// Implements: REQ-DATA-001 (migrations, per-power roles). Task PLT-06.
//
// May import: platform/{config,telemetry}.
//
// Must never: be imported by the model service, read the temporal database (INV-30), or use a floating-point type for money.
package db
