// Package grants is the one place a grant is built.
package grants

// DecisionGrant is a stand-in for the generated type.
type DecisionGrant struct{ ID string }

// Issue builds a grant, as allowed.
func Issue() DecisionGrant { return DecisionGrant{ID: "g"} }
