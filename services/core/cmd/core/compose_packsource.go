package main

import (
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/packload"
)

// compose_packsource.go is the production PackSource (STM-01 final composition, lane 1; rulings A333, A377 items 6 and 7, A469, A486, A510; interim, relayed, not yet committed;
// REQ-STM-001, REQ-TRU-004, REQ-TRU-005, REQ-OPS-003; INV-14, INV-16, INV-21; AGENT-13, AGENT-16). It loads the pack through its manifest (platform/packload: every file is pinned by its
// SHA-256, and so is every action graph the pack names) and builds the Trust adapters' Pack from those verified bytes alone (trustpack.LoadVerified): no file is read a second time,
// so a graph that changed after the check is never served. The settings are the Understanding half's own (EnvPackDir, EnvPackManifest, EnvPackMaxBytes: no new setting). With none of
// them the pack is a named gap: the process starts and /readyz names it. With some, or with an invalid one, or with a pack that does not verify or cannot be built, the process stops,
// and the error never carries a path, a value or a word of the pack.

// productionPack is the production PackSource.
func productionPack(env Env) (*trustpack.Pack, error) {
	if env.Getenv == nil {
		return nil, errPackNotConfigured // an environment that cannot be asked gives no setting: a named gap, never a value invented
	}
	dir, manifest, bound := env.Getenv(EnvPackDir), env.Getenv(EnvPackManifest), env.Getenv(EnvPackMaxBytes)
	if dir == "" && manifest == "" && bound == "" {
		return nil, errPackNotConfigured
	}
	for _, s := range []struct{ name, value string }{{EnvPackDir, dir}, {EnvPackManifest, manifest}, {EnvPackMaxBytes, bound}} {
		if s.value == "" {
			return nil, errors.New("cmd/core: the pack needs " + s.name)
		}
	}
	maxBytes, err := wholeNumber(env, EnvPackMaxBytes, " of bytes")
	if err != nil {
		return nil, err
	}
	verified, err := packload.Load(dir, manifest, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %s: the pack cannot be loaded: %w", EnvPackDir, err)
	}
	pack, err := trustpack.LoadVerified(verified.Pack, verified.ActionGraphs)
	if err != nil {
		return nil, refused("Trust pack", err)
	}
	return pack, nil
}
