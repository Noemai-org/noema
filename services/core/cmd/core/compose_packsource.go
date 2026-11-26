package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/packload"
)

// compose_packsource.go is the production PackSource (STM-01 final composition, lane 1; rulings A333, A377 items 6 and 7, A469, A486, A510; interim, relayed, not yet committed;
// REQ-STM-001, REQ-TRU-004, REQ-TRU-005, REQ-OPS-003; INV-14, INV-16, INV-21; AGENT-13, AGENT-16). It loads the pack through its manifest (platform/packload: every file is pinned by its
// SHA-256, and so is every action graph the pack names) and builds the Trust adapters' Pack from those verified bytes alone (trustpack.LoadVerified): no file is read a second time,
// so a graph that changed after the check is never served. The settings are the Understanding half's own (EnvPackDir, EnvPackManifest, EnvPackMaxBytes: no new setting). With none of
// them the pack is a named gap: the process starts and /readyz names it. With some (A571) the gap names each missing setting. With an invalid one, or with a pack that does not verify or cannot be built, the process stops,
// and the error never carries a path, a value or a word of the pack.

// packSettingsGap is the named gap of a partly given set of pack settings (A571, A469, A486; interim, relayed, not yet committed): it is errPackNotConfigured with the names of the
// settings that are missing, in the order dir, manifest, bound. Its text is fixed and carries no value.
type packSettingsGap struct{ Missing []string }

func (e *packSettingsGap) Error() string {
	return errPackNotConfigured.Error() + ": missing " + strings.Join(e.Missing, ", ")
}

// Unwrap makes it the plain gap for errors.Is.
func (e *packSettingsGap) Unwrap() error { return errPackNotConfigured }

// packBound reads NOEMA_PACK_MAX_BYTES when it is given; an empty one is not judged here (it is a missing setting).
func packBound(env Env, given string) (int64, error) {
	if given == "" {
		return 0, nil
	}
	return wholeNumber(env, EnvPackMaxBytes, " of bytes")
}

// productionPack is the production PackSource.
func productionPack(env Env) (*trustpack.Pack, error) {
	if env.Getenv == nil {
		return nil, errPackNotConfigured // an environment that cannot be asked gives no setting: a named gap, never a value invented
	}
	dir, manifest, bound := env.Getenv(EnvPackDir), env.Getenv(EnvPackManifest), env.Getenv(EnvPackMaxBytes)
	if dir == "" && manifest == "" && bound == "" {
		return nil, errPackNotConfigured
	}
	maxBytes, err := packBound(env, bound)
	if err != nil {
		return nil, err // an invalid present value is fatal whatever else is missing (A571)
	}
	var missing []string
	for _, s := range []struct{ name, value string }{{EnvPackDir, dir}, {EnvPackManifest, manifest}, {EnvPackMaxBytes, bound}} {
		if s.value == "" {
			missing = append(missing, s.name)
		}
	}
	if len(missing) > 0 {
		return nil, &packSettingsGap{Missing: missing}
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

// VerifiedSource gives the manifest-verified pack with the bytes of every file the manifest pins (A722): the same one load that the Trust pack is built from, so a consumer of the pinned
// bytes (the injection screen) never loads the pack again. An unset or partly set pack is errPackNotConfigured (a named gap); anything else is fatal.
type VerifiedSource func(Env) (packload.Pack, error)

// loadVerified reads the pack settings and loads the pack through its manifest (every file pinned by its SHA-256).
func loadVerified(env Env) (packload.Pack, error) {
	if env.Getenv == nil {
		return packload.Pack{}, errPackNotConfigured
	}
	dir, manifest, bound := env.Getenv(EnvPackDir), env.Getenv(EnvPackManifest), env.Getenv(EnvPackMaxBytes)
	if dir == "" && manifest == "" && bound == "" {
		return packload.Pack{}, errPackNotConfigured
	}
	maxBytes, err := packBound(env, bound)
	if err != nil {
		return packload.Pack{}, err
	}
	var missing []string
	for _, s := range []struct{ name, value string }{{EnvPackDir, dir}, {EnvPackManifest, manifest}, {EnvPackMaxBytes, bound}} {
		if s.value == "" {
			missing = append(missing, s.name)
		}
	}
	if len(missing) > 0 {
		return packload.Pack{}, &packSettingsGap{Missing: missing}
	}
	verified, err := packload.Load(dir, manifest, maxBytes)
	if err != nil {
		return packload.Pack{}, fmt.Errorf("cmd/core: %s: the pack cannot be loaded: %w", EnvPackDir, err)
	}
	return verified, nil
}

// packFromVerified builds the Trust pack from one verified load.
func packFromVerified(src VerifiedSource) PackSource {
	return func(env Env) (*trustpack.Pack, error) {
		verified, err := src(env)
		if err != nil {
			return nil, err
		}
		pack, err := trustpack.LoadVerified(verified.Pack, verified.ActionGraphs)
		if err != nil {
			return nil, refused("Trust pack", err)
		}
		return pack, nil
	}
}

// memoVerified answers once, a failure included (A548, INV-16).
func memoVerified(src VerifiedSource) VerifiedSource {
	var once sync.Once
	var v packload.Pack
	var err error
	return func(env Env) (packload.Pack, error) {
		once.Do(func() { v, err = src(env) })
		return v, err
	}
}
