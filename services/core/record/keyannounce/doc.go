// Package keyannounce appends KEY_ROTATED to a tenant's Record for every signing-key version that has none yet (SEC-KEY-ROTATED; REQ-SEC-005, REQ-AUD-001, REQ-AUD-006;
// DEC-2026-11-01-receipt-and-record-signing D2a, D3a, D4, test 20). The deploy step registers public keys through key_register and writes no Record event (it holds no
// signing key), so the core announces each registration when it starts: KEY_ROTATED, written by the emitter admin and signed under the admin key, naming the key by key_id only.
//
// May import: record/chain, platform/clock, the database driver. Must never: read or hold a key, register, retire or revoke a key (key_register, key_retire and key_revoke are
// the migration role's), put key material in a payload, or act on anything but the Record.
package keyannounce
