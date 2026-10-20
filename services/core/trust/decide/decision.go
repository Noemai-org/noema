package decide

// Decision is one Trust outcome with its reasons. It has no exported field and no exported constructor but the ones
// below, so every Decision in the system was built by a rule in this file.
type Decision struct {
	outcome       Outcome
	reasons       []ReasonCode
	authority     Authority
	intentVersion int
	reevaluateAt  int64
	clarification *Clarification
}

// Outcome is the decision's outcome.
func (d Decision) Outcome() Outcome { return d.outcome }

// Reasons returns a copy of the reason codes.
func (d Decision) Reasons() []ReasonCode { return append([]ReasonCode(nil), d.reasons...) }

// Authority is the three layer results recorded with the decision.
func (d Decision) Authority() Authority { return d.authority }

// IntentVersion is the Intent Object version decided on.
func (d Decision) IntentVersion() int { return d.intentVersion }

// ReevaluateAt is the time a WAIT decision is re-evaluated, in Unix seconds; zero for every other outcome.
func (d Decision) ReevaluateAt() int64 { return d.reevaluateAt }

// Clarification is the question a CLARIFY decision carries, or nil.
func (d Decision) Clarification() *Clarification { return d.clarification }

func build(o Outcome, version int, a Authority, reasons []ReasonCode) (Decision, error) {
	if err := checkReasons(reasons); err != nil {
		return Decision{}, err
	}
	return Decision{outcome: o, reasons: append([]ReasonCode(nil), reasons...), authority: a, intentVersion: version}, nil
}

// Allow builds an ALLOW. All three authority layers must be PASS.
func (r ResolvedIntent) Allow(a Authority, reasons ...ReasonCode) (Decision, error) {
	return r.withAuthority(Allow, a, reasons)
}

// Confirm builds a CONFIRM: permitted only if an authenticated approver confirms the exact bound action. All three
// authority layers must be PASS, because the confirmation is followed by a fresh evaluation, not a bypass.
func (r ResolvedIntent) Confirm(a Authority, reasons ...ReasonCode) (Decision, error) {
	return r.withAuthority(Confirm, a, reasons)
}

func (r ResolvedIntent) withAuthority(o Outcome, a Authority, reasons []ReasonCode) (Decision, error) {
	switch {
	case !r.ok:
		return Decision{}, ErrZeroIntent
	case !a.valid():
		return Decision{}, ErrAuthorityInvalid
	case !a.AllPass():
		return Decision{}, ErrAuthorityNotPass
	}
	return build(o, r.version, a, reasons)
}

// Reject builds a REJECT. It needs at least one reason code.
func (r ResolvedIntent) Reject(a Authority, reasons ...ReasonCode) (Decision, error) {
	return r.withReasons(Reject, a, reasons)
}

// Unknown builds an UNKNOWN: Trust cannot safely produce any other outcome. It needs at least one reason code.
func (r ResolvedIntent) Unknown(a Authority, reasons ...ReasonCode) (Decision, error) {
	return r.withReasons(Unknown, a, reasons)
}

// Escalate builds an ESCALATE for a resolved intent.
func (r ResolvedIntent) Escalate(a Authority, reasons ...ReasonCode) (Decision, error) {
	return r.withReasons(Escalate, a, reasons)
}

func (r ResolvedIntent) withReasons(o Outcome, a Authority, reasons []ReasonCode) (Decision, error) {
	switch {
	case !r.ok:
		return Decision{}, ErrZeroIntent
	case !a.valid():
		return Decision{}, ErrAuthorityInvalid
	case len(reasons) == 0:
		return Decision{}, ErrNoReason
	}
	return build(o, r.version, a, reasons)
}

// Wait builds a WAIT: a bounded, known condition must first change. It needs a re-evaluation time.
func (r ResolvedIntent) Wait(a Authority, reevaluateAt int64, reasons ...ReasonCode) (Decision, error) {
	if reevaluateAt <= 0 {
		return Decision{}, ErrNoReevaluateAt
	}
	d, err := r.withReasons(Wait, a, reasons)
	d.reevaluateAt = reevaluateAt
	return d, err
}

// Clarify builds a CLARIFY carrying the question. It is one of the two decisions an unresolved intent can have.
func (u UnresolvedIntent) Clarify(q Clarification, reasons ...ReasonCode) (Decision, error) {
	if !u.ok {
		return Decision{}, ErrZeroIntent
	}
	if len(reasons) == 0 {
		return Decision{}, ErrNoReason
	}
	d, err := build(Clarify, u.version, Authority{Technical: Pass, Policy: NotEvaluated, Business: NotEvaluated}, reasons)
	d.clarification = &q
	return d, err
}

// Escalate builds an ESCALATE for an unresolved intent: a handoff, because no target is resolved.
func (u UnresolvedIntent) Escalate(reasons ...ReasonCode) (Decision, error) {
	if !u.ok {
		return Decision{}, ErrZeroIntent
	}
	if len(reasons) == 0 {
		return Decision{}, ErrNoReason
	}
	return build(Escalate, u.version, Authority{Technical: Pass, Policy: NotEvaluated, Business: NotEvaluated}, reasons)
}
