package candidaterank

import "strings"

// ParseBP reads a pack number as an exact number of basis points: a decimal such as "0.35" or "1" (at most four digits after the point, so that every one is a whole basis point), or
// a fraction such as "7/20" that is a whole number of basis points. It reads the text and never makes a float: a pack's JSON number is passed as the text it is written in. A sign,
// an exponent, a number above 1, and a value that is not a whole number of basis points are refused (ErrNumber).
func ParseBP(text string) (int64, error) {
	if i := strings.IndexByte(text, '/'); i >= 0 {
		num, okN := digits(text[:i])
		den, okD := digits(text[i+1:])
		if !okN || !okD || den == 0 || num > den {
			return 0, ErrNumber
		}
		scaled := num * UnitBP
		if scaled/UnitBP != num || scaled%den != 0 {
			return 0, ErrNumber
		}
		return scaled / den, nil
	}
	whole, frac := text, ""
	if i := strings.IndexByte(text, '.'); i >= 0 {
		whole, frac = text[:i], text[i+1:]
		if frac == "" || len(frac) > 4 {
			return 0, ErrNumber
		}
	}
	w, ok := digits(whole)
	if !ok || w > 1 {
		return 0, ErrNumber
	}
	value := w * UnitBP
	scale := UnitBP / 10
	for _, ch := range frac {
		if ch < '0' || ch > '9' {
			return 0, ErrNumber
		}
		value += int64(ch-'0') * scale
		scale /= 10
	}
	if value > UnitBP {
		return 0, ErrNumber
	}
	return value, nil
}

// digits reads a non-empty run of ASCII digits of at most eighteen, so that it fits an int64.
func digits(s string) (int64, bool) {
	if s == "" || len(s) > 18 {
		return 0, false
	}
	var v int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		v = v*10 + int64(ch-'0')
	}
	return v, true
}
