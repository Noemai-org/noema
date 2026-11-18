package main

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// investigationStores gives the investigation store of one tenant. Stub for the red commit.
type investigationStores interface {
	For(tenantID string) (*investigations.Store, error)
}

type unbuiltReader struct{}

func (unbuiltReader) Read(context.Context, string, string, string) (orchestrator.State, bool, error) {
	return "", false, ErrInvestigationRead
}

// newInvestigationReader is the production InvestigationReader. Stub for the red commit.
func newInvestigationReader(investigationStores) InvestigationReader { return unbuiltReader{} }

// ErrInvestigationRead marks a read that failed: the store could not be reached or answered something it should not. It carries no driver text (AGENT-13).
var ErrInvestigationRead = errors.New("cmd/core: the investigation could not be read")
