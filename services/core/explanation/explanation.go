// Package explanation is the deterministic side of REQ-EXP-002: the fallback renderer from pack templates, and the claim validator of PERD 31.2. No model, no network,
// no clock. Templates are pack data read as JSON (SoT 51); nothing here imports pack code, and nothing in trust, execution or outcome imports this package (AGENT-05).
package explanation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const (
	hashPrefix   = "sha256:"
	maxDigitRun  = 4 // PERD 31.2 check 4: no unmasked digit run longer than 4
	templatePath = "%s.json"
)

// ErrNoTemplate means there is no template for the locale, outcome and classification. The caller blocks the explanation; nothing is guessed or borrowed from another locale.
var ErrNoTemplate = errors.New("explanation: no template")

type entry struct{ text, version string }

// Templates are the loaded pack templates: locale, then "<outcome_id>/<classification>".
type Templates struct{ byLocale map[string]map[string]entry }

// LoadTemplates reads <dir>/<locale>.json for each locale. A missing file, a file that is not a JSON object of strings, or an empty text is an error.
func LoadTemplates(dir string, locales []string) (Templates, error) {
	t := Templates{byLocale: map[string]map[string]entry{}}
	for _, loc := range locales {
		raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(templatePath, loc))) // #nosec G304 -- the directory is configuration
		if err != nil {
			return Templates{}, fmt.Errorf("explanation: template file for %s: %w", loc, err)
		}
		var doc map[string]string
		if err := json.Unmarshal(raw, &doc); err != nil {
			return Templates{}, fmt.Errorf("explanation: %s is not an object of strings: %w", loc, err)
		}
		sum := sha256.Sum256(raw)
		version := hashPrefix + hex.EncodeToString(sum[:])
		m := make(map[string]entry, len(doc))
		for key, text := range doc {
			if strings.TrimSpace(text) == "" {
				return Templates{}, fmt.Errorf("explanation: the %s template for %s is empty", loc, key)
			}
			m[key] = entry{text: text, version: version}
		}
		t.byLocale[loc] = m
	}
	return t, nil
}

// Keys lists a locale's template keys, sorted.
func (t Templates) Keys(locale string) []string {
	var keys []string
	for k := range t.byLocale[locale] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Claim binds a span of the text to the facts or the template that support it (PERD 31.2).
type Claim struct {
	Span       string
	FactIDs    []string
	TemplateID string
}

// Rendered is the renderer's answer: the text, what binds it, and which template and version it came from.
type Rendered struct {
	Text            string
	Claims          []Claim
	TemplateID      string
	TemplateVersion string
}

// Fallback renders the pack template for the facts' locale, outcome and classification. The whole text is one template-bound claim.
func Fallback(t Templates, f contracts.ExplanationFactsV2) (Rendered, error) {
	locale := string(f.Locale)
	key := string(f.OutcomeId) + "/" + f.Classification
	e, ok := t.byLocale[locale][key]
	if !ok {
		return Rendered{}, fmt.Errorf("%w for %s/%s", ErrNoTemplate, locale, key)
	}
	id := locale + "/" + key
	return Rendered{Text: e.text, TemplateID: id, TemplateVersion: e.version, Claims: []Claim{{Span: e.text, TemplateID: id}}}, nil
}

var (
	sentenceEnd = regexp.MustCompile(`[.!?]+\s+|[.!?]+$`)
	figure      = regexp.MustCompile(`[0-9][0-9,.:]*[0-9]|[0-9]`)
	digitRun    = regexp.MustCompile(`[0-9]+`)
	capitalised = regexp.MustCompile(`\b[A-Z][A-Za-z'’-]*`)
)

// openers are the capitalised words of ordinary English that may begin or sit in a sentence without being a name: pronouns, articles, auxiliaries and the like. It is
// language syntax, not domain data. Any other capitalised word must occur in the facts or in the rendered template, so an invented person or institution is blocked
// (REQ-EXP-002 INVENTED_NAME). The check is deliberately strict: a false block falls back to the template.
var openers = map[string]bool{"A": true, "An": true, "The": true, "This": true, "That": true, "These": true, "Those": true, "It": true, "Its": true, "We": true, "Our": true,
	"Your": true, "You": true, "There": true, "Here": true, "If": true, "Please": true, "No": true, "Not": true, "And": true, "But": true, "Or": true, "Is": true, "Are": true,
	"Was": true, "Were": true, "Has": true, "Have": true, "Had": true, "Will": true, "Can": true, "Could": true, "May": true, "About": true, "For": true, "On": true, "At": true,
	"In": true, "To": true, "Of": true, "As": true, "By": true, "With": true, "Do": true, "Does": true, "Did": true}

func factStrings(f contracts.ExplanationFactsV2) []string {
	var out []string
	for _, fact := range f.Facts {
		out = append(out, fact.ValueMasked, fact.SourceLabel, string(fact.AsOf))
	}
	return out
}

// normalFigure drops thousands separators, so 50,000 and 50000 are one figure and 50 is not 50,000.
func normalFigure(s string) string { return strings.ReplaceAll(s, ",", "") }

// figureSet is every whole figure in the fact strings, plus the hour:minute prefix of each time (10:42:00 also allows 10:42).
func figureSet(strs []string) map[string]bool {
	set := map[string]bool{}
	for _, s := range strs {
		for _, fig := range figure.FindAllString(s, -1) {
			n := normalFigure(fig)
			set[n] = true
			if parts := strings.Split(n, ":"); len(parts) == 3 {
				set[parts[0]+":"+parts[1]] = true
			}
		}
	}
	return set
}

func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range capitalised.FindAllString(s, -1) {
		set[w] = true
	}
	return set
}

// Validate runs the four checks of PERD 31.2 and the forbidden-claim patterns (pack data, passed in). It returns every problem; none means the text may be shown.
// 1. every sentence is covered by a claim bound to a fact of the input or to the rendered template; 2. every figure, time or masked identifier in the text appears in
// the facts; 3. no forbidden pattern matches; 4. no unmasked digit run longer than 4.
func Validate(t Templates, r Rendered, f contracts.ExplanationFactsV2, forbidden []string) []string {
	var out []string
	known := map[string]bool{}
	for _, fact := range f.Facts {
		known[fact.FactId] = true
	}
	covered := make([]string, 0, len(r.Claims))
	for i, c := range r.Claims {
		if !strings.Contains(r.Text, c.Span) || strings.TrimSpace(c.Span) == "" {
			out = append(out, fmt.Sprintf("claim %d: its span is not in the text", i))
			continue
		}
		switch {
		case c.TemplateID != "" && len(c.FactIDs) > 0:
			out = append(out, fmt.Sprintf("claim %d: bound to both a template and facts", i))
		case c.TemplateID != "":
			if c.TemplateID != r.TemplateID || r.TemplateID == "" {
				out = append(out, fmt.Sprintf("claim %d: bound to template %q, not the rendered template", i, c.TemplateID))
				continue
			}
			if want, ok := t.text(c.TemplateID); !ok || !strings.Contains(want, c.Span) {
				out = append(out, fmt.Sprintf("claim %d: its span is not the text of template %q", i, c.TemplateID))
				continue
			}
		case len(c.FactIDs) > 0:
			bad := false
			for _, id := range c.FactIDs {
				if !known[id] {
					out = append(out, fmt.Sprintf("claim %d: fact %q is not in the input", i, id))
					bad = true
				}
			}
			if bad {
				continue
			}
		default:
			out = append(out, fmt.Sprintf("claim %d: bound to neither a fact nor a template", i))
			continue
		}
		covered = append(covered, c.Span)
	}
	for _, s := range sentences(r.Text) {
		ok := false
		for _, span := range covered {
			if strings.Contains(span, s) {
				ok = true
				break
			}
		}
		if !ok {
			out = append(out, fmt.Sprintf("check 1: the sentence %q is not covered by a valid claim", s))
		}
	}
	allowed := figureSet(factStrings(f))
	for _, fig := range figure.FindAllString(r.Text, -1) {
		if !allowed[normalFigure(fig)] && !coveredByTemplateOnly(t, r, fig) {
			out = append(out, fmt.Sprintf("check 2: %q is not a figure of the facts", fig))
		}
	}
	words := wordSet(strings.Join(factStrings(f), "\n"))
	if want, ok := t.text(r.TemplateID); ok {
		for w := range wordSet(want) {
			words[w] = true
		}
	}
	for _, w := range capitalised.FindAllString(r.Text, -1) {
		if !words[w] && !openers[w] {
			out = append(out, fmt.Sprintf("check 2: %q is a name or term that is not in the facts", w))
		}
	}
	for _, p := range forbidden {
		re, err := regexp.Compile(p)
		if err != nil {
			out = append(out, fmt.Sprintf("check 3: the forbidden pattern %q is invalid", p))
			continue
		}
		if re.MatchString(r.Text) {
			out = append(out, fmt.Sprintf("check 3: matches the forbidden pattern %q", p))
		}
	}
	for _, run := range digitRun.FindAllString(r.Text, -1) {
		if len(run) > maxDigitRun {
			out = append(out, fmt.Sprintf("check 4: an unmasked digit run of %d", len(run)))
		}
	}
	return out
}

// coveredByTemplateOnly reports whether a figure sits only inside spans bound to the rendered template: pack text is reviewed data and not a model's claim.
func coveredByTemplateOnly(t Templates, r Rendered, fig string) bool {
	for _, c := range r.Claims {
		if want, ok := t.text(c.TemplateID); ok && c.TemplateID == r.TemplateID && strings.Contains(want, c.Span) && strings.Contains(c.Span, fig) {
			return true
		}
	}
	return false
}

// text finds the template text for an ID of the form "<locale>/<outcome_id>/<classification>".
func (t Templates) text(id string) (string, bool) {
	locale, key, ok := strings.Cut(id, "/")
	if !ok {
		return "", false
	}
	e, found := t.byLocale[locale][key]
	return e.text, found
}

func sentences(text string) []string {
	var out []string
	last := 0
	for _, loc := range sentenceEnd.FindAllStringIndex(text, -1) {
		if s := strings.TrimSpace(text[last:loc[1]]); s != "" {
			out = append(out, s)
		}
		last = loc[1]
	}
	if s := strings.TrimSpace(text[last:]); s != "" {
		out = append(out, s)
	}
	return out
}
