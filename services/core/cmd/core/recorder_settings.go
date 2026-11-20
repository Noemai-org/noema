package main

import "time"

// recorder_settings.go: the three required settings of the Decision Record writer (STM-01; ruling A475, interim, relayed, recorded in docs#216; REQ-TRU-008, REQ-TRU-009;
// AGENT-16, A330). This is the red stub: it compiles and refuses nothing, so the tests fail on the missing behaviour.

const (
	// EnvTrustGrantLifetimeMS is how long a Decision Grant is usable after it is evaluated, in whole milliseconds. Required, no default.
	EnvTrustGrantLifetimeMS = "NOEMA_TRUST_GRANT_LIFETIME_MS"
	// EnvTrustRecordLifetimeMS is how long a Decision Record is usable after it is issued, in whole milliseconds. Required, no default.
	EnvTrustRecordLifetimeMS = "NOEMA_TRUST_RECORD_LIFETIME_MS"
	// EnvTrustTxTimeoutMS bounds the whole of one Record write, in whole milliseconds. Required, no default.
	EnvTrustTxTimeoutMS = "NOEMA_TRUST_TX_TIMEOUT_MS"
)

// RecorderSettings are the three lifetimes of the Recorder, from settings only.
type RecorderSettings struct {
	GrantLifetime  time.Duration
	RecordLifetime time.Duration
	TxTimeout      time.Duration
}

// loadRecorderSettings reads the three settings.
func loadRecorderSettings(env Env) (RecorderSettings, error) {
	return RecorderSettings{}, nil
}
