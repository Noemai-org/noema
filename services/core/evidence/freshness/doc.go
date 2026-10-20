// Package freshness: Effective freshness at decision and at revalidation: the stricter of the source's
// limit and the trust profile's limit.
//
// Implements: REQ-CTX-003.
//
// May import: platform/{clock,config}, internal/contracts.
//
// Must never: relax a freshness limit; read the wall clock except through platform/clock.
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package freshness
