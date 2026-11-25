// Package candidaterank is the deterministic ranker of REQ-AMB-003 for the Trust side (ruling A555, interim, relayed; A559 and A560): integer basis points, floor division, no float.
//
// It is in platform/ and not in trust/ because the lint forbids a numeric literal other than 0 and 1 in decision code under trust/, and the unit of a basis point is 10000
// (platform/trustlimits and platform/ceilings are placed the same way). Every threshold, weight and tolerance is an input (Config); nothing that decides is written here.
//
// May import: the standard library.
//
// Must never: call a model, read a clock, a file, a network or a database, use a float, or name an industry or a country (INV-01, INV-14, INV-21).
package candidaterank
