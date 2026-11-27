package recipientmatch

import (
	"errors"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/Noemai-org/noema/services/core/evidence/candidates_api"
)

// ErrShare is returned when a share cannot be an integer fraction of the port's type (a numerator or denominator past an int64). Its text is fixed and carries no part of a name.
var ErrShare = errors.New("recipientmatch: the share does not fit")

// maxTokens: names with more tokens are cut to their first tokens. The cut can only lower or keep a score (the reference matcher's rule).
const maxTokens = 8

var apostrophes = map[rune]bool{'\'': true, '’': true, 'ʼ': true}

// Matcher is the recipient matcher built from the pack's data. It holds no state that changes: it may be used by many readers at once.
type Matcher struct {
	tiers   [5]*big.Rat
	foldMap map[rune]string
	aliases map[[2]string]bool
	minKey  int
	vowels  map[rune]bool
	rules   [][2]string
}

var _ candidates_api.RecipientMatcher = (*Matcher)(nil)

type token struct{ raw, folded string }

// Match is the port of the Candidates read: the share with the phonetic tier off. The request has no phonetic member, so that tier is not available here and counts as no match for that tier.
func (m *Matcher) Match(surface, name string) (candidates_api.Match, error) {
	return m.Score(surface, name, false)
}

// Score is the recipient feature in [0, 1]: how well name matches query, as an exact fraction in lowest terms. It is 0/1 for an empty query or when either side has no token. phonetic enables the
// voice path's tier. A share that does not fit an int64 fraction is ErrShare.
func (m *Matcher) Score(query, name string, phonetic bool) (candidates_api.Match, error) {
	if query == "" {
		return candidates_api.Match{Numerator: 0, Denominator: 1}, nil
	}
	q, c := m.tokens(query), m.tokens(name)
	if len(q) == 0 || len(c) == 0 {
		return candidates_api.Match{Numerator: 0, Denominator: 1}, nil
	}
	grid := make([][]*big.Rat, len(q))
	for i, qt := range q {
		grid[i] = make([]*big.Rat, len(c))
		for j, ct := range c {
			grid[i][j] = m.tokenScore(qt, ct, phonetic)
		}
	}
	total := best(grid, len(c))
	share := new(big.Rat).Quo(total, new(big.Rat).SetInt64(int64(len(q))))
	if !share.Num().IsInt64() || !share.Denom().IsInt64() {
		return candidates_api.Match{}, ErrShare
	}
	return candidates_api.Match{Numerator: share.Num().Int64(), Denominator: share.Denom().Int64()}, nil
}

// best is the largest total of a one-to-one assignment of query tokens to name tokens, a query token being allowed to stay unmatched.
func best(grid [][]*big.Rat, m int) *big.Rat {
	n := len(grid)
	memo := make([]map[int]*big.Rat, n+1)
	for i := range memo {
		memo[i] = map[int]*big.Rat{}
	}
	var solve func(i, used int) *big.Rat
	solve = func(i, used int) *big.Rat {
		if i == n {
			return new(big.Rat)
		}
		if v, ok := memo[i][used]; ok {
			return v
		}
		top := solve(i+1, used) // this query token stays unmatched
		for j := 0; j < m; j++ {
			if used&(1<<j) != 0 {
				continue
			}
			if v := new(big.Rat).Add(grid[i][j], solve(i+1, used|1<<j)); v.Cmp(top) > 0 {
				top = v
			}
		}
		memo[i][used] = top
		return top
	}
	return solve(0, 0)
}

// tokenScore is the best tier two tokens reach, in the order exact, folded, alias, phonetic (voice path only), initial.
func (m *Matcher) tokenScore(q, c token, phonetic bool) *big.Rat {
	switch {
	case q.raw == c.raw:
		return m.tiers[0]
	case q.folded == c.folded:
		return m.tiers[1]
	case m.aliases[[2]string{q.folded, c.folded}]:
		return m.tiers[2]
	}
	if phonetic {
		if key := m.phoneticKey(q.folded); utf8.RuneCountInString(key) >= m.minKey && key == m.phoneticKey(c.folded) {
			return m.tiers[3]
		}
	}
	short, long := q.folded, c.folded
	if utf8.RuneCountInString(long) < utf8.RuneCountInString(short) {
		short, long = long, short
	}
	if utf8.RuneCountInString(short) == 1 && utf8.RuneCountInString(long) > 1 && strings.HasPrefix(long, short) {
		return m.tiers[4]
	}
	return new(big.Rat)
}

// tokens splits a name into its letter and mark runs (an apostrophe stays in a word), keeps the first maxTokens whose folded form is not empty, and drops the rest. Text that is not valid UTF-8
// has each bad byte read as the replacement character, which is not a letter, so hostile bytes only separate words.
func (m *Matcher) tokens(name string) []token {
	name = norm.NFC.String(strings.ToValidUTF8(name, "�"))
	var out []token
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		w := string(word)
		word = word[:0]
		if f := m.fold(w); f != "" {
			out = append(out, token{raw: cases.Fold().String(norm.NFC.String(w)), folded: f})
		}
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsMark(r) || apostrophes[r] {
			word = append(word, r)
			continue
		}
		flush()
		if len(out) >= maxTokens {
			return out
		}
	}
	flush()
	if len(out) > maxTokens {
		out = out[:maxTokens]
	}
	return out
}

// fold is NFC, case folding, NFKD, with the nonspacing marks dropped and then the pack's folding map applied character by character.
func (m *Matcher) fold(text string) string {
	decomposed := norm.NFKD.String(cases.Fold().String(norm.NFC.String(text)))
	var b strings.Builder
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if to, ok := m.foldMap[r]; ok {
			b.WriteString(to)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// phoneticKey applies the pack's rules in order, collapses repeated characters, and drops the vowels after the first character.
func (m *Matcher) phoneticKey(folded string) string {
	text := folded
	for _, rule := range m.rules {
		text = strings.ReplaceAll(text, rule[0], rule[1])
	}
	runes := []rune(text)
	var collapsed []rune
	for i, r := range runes {
		if i == 0 || r != runes[i-1] {
			collapsed = append(collapsed, r)
		}
	}
	var key []rune
	for i, r := range collapsed {
		if i == 0 || !m.vowels[r] {
			key = append(key, r)
		}
	}
	return string(key)
}
