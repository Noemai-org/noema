package main

// A739 (interim, committed on docs main): the upper bound of every key ring's overlap is 999, the Execution ring's bound. One parser, so no ring has its own pattern.

import (
	"fmt"
	"regexp"
	"strconv"
)

var keyOverlapPattern = regexp.MustCompile(`^[1-9][0-9]{0,2}$`)

// parseKeyOverlap reads the rotation overlap of a key ring: a whole number from 1 to 999. The error names the setting and never the value.
func parseKeyOverlap(name, value string) (int, error) {
	if !keyOverlapPattern.MatchString(value) {
		return 0, fmt.Errorf("cmd/core: %s must be a whole number from 1 to 999", name)
	}
	n, err := strconv.Atoi(value) // the pattern holds it to at most three digits
	if err != nil {
		return 0, fmt.Errorf("cmd/core: %s must be a whole number from 1 to 999", name)
	}
	return n, nil
}
