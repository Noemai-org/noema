package recipientmatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ErrData is returned for matcher data that is not exactly the shape the matcher reads. Its text is fixed: it names no member and carries no value.
var ErrData = errors.New("recipientmatch: the matcher data is not usable")

// acceptedMarks are the only marks an entry may carry: every entry is a synthetic seed or has been reviewed by a native speaker (the reference matcher's rule).
var acceptedMarks = map[string]bool{"[synthetic seed]": true, "[native-speaker reviewed]": true}

// numberRe is a non-negative decimal or fraction written as a string, as the reference matcher reads a pack number (never a float).
var numberRe = regexp.MustCompile(`^(?:[0-9]{1,18}(?:\.[0-9]{1,18})?|[0-9]{1,18}/[1-9][0-9]{0,17})$`)

var integerRe = regexp.MustCompile(`^[0-9]+$`)

// tierOrder is the order tokens are compared in and the order the scores may never rise in.
var tierOrder = [5]string{"exact", "folded", "alias", "phonetic", "initial"}

type rawFile struct {
	MatcherID string                     `json:"matcher_id"`
	Version   string                     `json:"version"`
	Mark      string                     `json:"mark"`
	Note      *string                    `json:"note"`
	Tiers     map[string]json.RawMessage `json:"tiers"`
	Fold      *struct {
		Mark string            `json:"mark"`
		Map  map[string]string `json:"map"`
	} `json:"fold"`
	Nicknames []struct {
		Nickname  string   `json:"nickname"`
		FullNames []string `json:"full_names"`
		Locale    *string  `json:"locale"`
		Mark      string   `json:"mark"`
	} `json:"nicknames"`
	Variants []struct {
		Names  []string `json:"names"`
		Locale *string  `json:"locale"`
		Mark   string   `json:"mark"`
	} `json:"variants"`
	Phonetic *struct {
		Mark         string     `json:"mark"`
		MinKeyLength *int       `json:"min_key_length"`
		Vowels       string     `json:"vowels"`
		Rules        [][]string `json:"rules"`
	} `json:"phonetic"`
}

// Load reads and checks the matcher data and builds the matcher. Anything missing, unknown, unmarked, or off its shape refuses the whole file; nothing is half-loaded.
func Load(data []byte) (*Matcher, error) {
	var f rawFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, ErrData
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrData
	}
	if f.MatcherID == "" || f.Version == "" || !acceptedMarks[f.Mark] || f.Tiers == nil || f.Fold == nil || f.Phonetic == nil || f.Nicknames == nil || f.Variants == nil {
		return nil, ErrData
	}
	if f.Note != nil && *f.Note == "" {
		return nil, ErrData
	}
	m := &Matcher{foldMap: map[rune]string{}, aliases: map[[2]string]bool{}, vowels: map[rune]bool{}}
	if err := m.loadTiers(f.Tiers); err != nil {
		return nil, err
	}
	if !acceptedMarks[f.Fold.Mark] || f.Fold.Map == nil {
		return nil, ErrData
	}
	for k, v := range f.Fold.Map {
		if utf8.RuneCountInString(k) != 1 {
			return nil, ErrData
		}
		r, _ := utf8.DecodeRuneInString(k)
		m.foldMap[r] = v
	}
	p := f.Phonetic
	if !acceptedMarks[p.Mark] || p.MinKeyLength == nil || *p.MinKeyLength < 1 || p.Vowels == "" || p.Rules == nil {
		return nil, ErrData
	}
	m.minKey = *p.MinKeyLength
	for _, r := range p.Vowels {
		m.vowels[r] = true
	}
	for _, rule := range p.Rules {
		if len(rule) != 2 || rule[0] == "" {
			return nil, ErrData
		}
		m.rules = append(m.rules, [2]string{rule[0], rule[1]})
	}
	for _, e := range f.Nicknames {
		if !acceptedMarks[e.Mark] || e.Nickname == "" || len(e.FullNames) == 0 {
			return nil, ErrData
		}
		nick := m.fold(e.Nickname)
		for _, full := range e.FullNames {
			if full == "" {
				return nil, ErrData
			}
			ff := m.fold(full)
			m.aliases[[2]string{nick, ff}], m.aliases[[2]string{ff, nick}] = true, true
		}
	}
	for _, e := range f.Variants {
		if !acceptedMarks[e.Mark] || len(e.Names) < 2 {
			return nil, ErrData
		}
		folded := make([]string, len(e.Names))
		for i, n := range e.Names {
			if n == "" {
				return nil, ErrData
			}
			folded[i] = m.fold(n)
		}
		for _, a := range folded {
			for _, b := range folded {
				if a != b {
					m.aliases[[2]string{a, b}] = true
				}
			}
		}
	}
	return m, nil
}

// loadTiers reads the five tier scores as exact numbers: each in (0, 1], never rising from exact down to initial.
func (m *Matcher) loadTiers(raw map[string]json.RawMessage) error {
	var mark string
	if v, ok := raw["mark"]; !ok || json.Unmarshal(v, &mark) != nil || !acceptedMarks[mark] {
		return ErrData
	}
	if len(raw) != len(tierOrder)+1 {
		return ErrData // a member that is not a tier
	}
	one := big.NewRat(1, 1)
	for i, name := range tierOrder {
		v, ok := raw[name]
		if !ok {
			return ErrData
		}
		r, err := exactNumber(v)
		if err != nil || r.Sign() <= 0 || r.Cmp(one) > 0 || (i > 0 && r.Cmp(m.tiers[i-1]) > 0) {
			return ErrData
		}
		m.tiers[i] = r
	}
	return nil
}

// exactNumber reads a pack number: a string such as "0.35" or "7/20", or a non-negative integer. A float, an exponent, a sign or anything else is refused.
func exactNumber(raw json.RawMessage) (*big.Rat, error) {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, `"`) {
		var s string
		if json.Unmarshal(raw, &s) != nil || !numberRe.MatchString(s) {
			return nil, ErrData
		}
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			return nil, ErrData
		}
		return r, nil
	}
	if !integerRe.MatchString(text) {
		return nil, ErrData
	}
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, ErrData
	}
	return r, nil
}
