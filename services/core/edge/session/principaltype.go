package session

// PrincipalTypeOf is the single mapping from a stored role name to the principal kind of the Intent Object (ruling A149), for a caller that holds a role the edge verified
// earlier and stored (recovery, A490, interim, relayed). It is false for a name that is not one of the four roles of ruling A1; it never guesses.
func PrincipalTypeOf(role string) (string, bool) {
	return "", false
}
