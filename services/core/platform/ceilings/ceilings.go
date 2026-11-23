// Package ceilings holds the hard bounds that a pack may tighten and never loosen: the two upper bounds on what Trust shows a principal in a clarification question (A205 Q1,
// REQ-AMB-004) and the bounds of the two provisional trust limits (A233, interim).
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

	// MinAmbiguityAbsoluteFloorBP is the least value of the ambiguity absolute floor a pack may carry, in basis points: the PERD floor 0.70 (v1.23 line 1772, REQ-AMB-003) is a floor
	// that "can only tighten the SoT margin", so a pack may raise it and never lower it (ruling A233, interim, relayed; GOV-06 adopts the value).
	MinAmbiguityAbsoluteFloorBP = 7000

	// MinClockAtRiskRemainingBP and MaxClockAtRiskRemainingBP bound the AT_RISK share of a clock's duration, in basis points: 1000 to 5000 (ruling A233, interim, relayed). The PERD
	// value 25 percent (2500, [REC], v1.23 line 2134) lies inside; below the least a clock is almost never AT_RISK, above the most almost always.
	MinClockAtRiskRemainingBP = 1000
	MaxClockAtRiskRemainingBP = 5000

	// MaxConfirmationTTLMillis is the most a pack's confirmation TTL ceiling may be, in milliseconds: the PERD's 10 minutes (v1.23 section 67, the provisional defaults, row "Grants and
	// confirmations": "confirmation TTL 10 min", adopted by DEC-2026-10-17, OD-P-31; ruling A561, interim). A pack may shorten it and never lengthen it. It is the same number as the
	// `maximum` of confirmation_ttl_ceiling_ms in trust-limits.v1; ceilings_test.go fails if the two ever differ.
	MaxConfirmationTTLMillis = 600000
)
