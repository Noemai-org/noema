package main

import (
	"fmt"
	"strconv"
	"time"
)

// recorder_settings.go: the three required settings of the Decision Record writer (STM-01; ruling A475, interim, relayed, recorded in docs#216; REQ-TRU-008, REQ-TRU-009;
// AGENT-16, A330). Each is a whole number of milliseconds, required, with no default in code: the value is Compose's or the pack's, and the Founder's to confirm. A missing or
// invalid value is refused with an error that names the setting and never echoes the value (AGENT-13). The caller passes the three durations to trustport.RecorderConfig.

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

// recorderSettingMS reads one setting strictly: a whole positive number, no sign, no space, no leading zero, ASCII digits only, representable as a duration. The error names the
// setting and never echoes the value.
func recorderSettingMS(env Env, name string) (time.Duration, error) {
	raw := env.Getenv(name)
	ms, err := strconv.ParseInt(raw, 10, 64)
	if !readTimeoutRe.MatchString(raw) || err != nil || ms < 1 || ms > maxReadTimeoutMS {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number of milliseconds", name)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// loadRecorderSettings reads the three settings, in the order grant, record, transaction. On an error it returns the zero value and names the first setting that is missing or
// invalid. Once all three are valid, a grant lifetime above the record lifetime is refused (a grant must not outlive its record; Founder instruction, interim, relayed, not yet
// committed, A503): the error names both settings and no value. Equal values are accepted.
func loadRecorderSettings(env Env) (RecorderSettings, error) {
	var s RecorderSettings
	var err error
	if s.GrantLifetime, err = recorderSettingMS(env, EnvTrustGrantLifetimeMS); err != nil {
		return RecorderSettings{}, err
	}
	if s.RecordLifetime, err = recorderSettingMS(env, EnvTrustRecordLifetimeMS); err != nil {
		return RecorderSettings{}, err
	}
	if s.TxTimeout, err = recorderSettingMS(env, EnvTrustTxTimeoutMS); err != nil {
		return RecorderSettings{}, err
	}
	if s.GrantLifetime > s.RecordLifetime {
		return RecorderSettings{}, fmt.Errorf("cmd/core: %s must not exceed %s", EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS)
	}
	return s, nil
}
