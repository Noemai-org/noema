// Package ceilings holds the two hard upper bounds that a pack may tighten and never loosen on what Trust shows a principal in a clarification question (A205 Q1, REQ-AMB-004).
// They are in code on purpose: a pack value is data and arrives through a port, so the bound has to stand on its own, outside any decision code (the lint forbids a numeric
// literal in code under trust/). A pack that asks for more is refused by the step that reads it, not clamped.
//
// May import: nothing.
//
// Must never: hold a default for a pack value, or be changed without a decision record (AGENT-16): each bound cites the text that fixes it.
package ceilings

const (
	// MaxClarificationOptions is the most options a clarification question may list; more candidates above the floor escalate. PERD v1.24 lines 1810 ("options[] <= 4"), 1891 and
	// 1892 (2 to 4 candidates clarify, more than 4 escalate) and 6063 (changing it needs a full benchmark run and a decision record).
	MaxClarificationOptions = 4

	// MaxLabelDigitRun is the longest run of consecutive digits an option label may show: the last four of an account or transfer (AGENTS section 7, PERD 31.2; ruling A154 as
	// corrected, which caps every outward surface at four). It is the same number as the const of masking-limits.v1; ceilings_test.go fails if the two ever differ.
	MaxLabelDigitRun = 4
)
