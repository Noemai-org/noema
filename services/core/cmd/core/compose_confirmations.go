package main

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// EnvConfirmationTimeout bounds one call of the confirmation store, in whole milliseconds. Required, no default (AGENT-16).
const EnvConfirmationTimeout = "NOEMA_CONFIRMATION_TIMEOUT_MS"

func buildConfirmations(env Env, clk clock.Clock, gen *ids.Generator, logins map[string]*pgxpool.Pool, app *chain.Appender, log interface{}) (*confirmationRouteConfig, string, error) {
	return nil, "confirmations: not built", nil
}

func mapConfirmationError(err error) error { return err }
