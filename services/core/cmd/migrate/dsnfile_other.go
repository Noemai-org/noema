//go:build !unix

package main

// readDSN is not available off unix: the checks that protect the DSN file (no link followed, the owner and the mode read from the open descriptor) have no portable form, so the command refuses
// rather than read a password without them. The command is a Linux container one-shot (A484).
func readDSN(string) (string, error) { return "", errDSNFile }
