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
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/masking"
)

const (
	hashPrefix   = "sha256:"
	templatePath = "%s.json"
)

// ErrNoTemplate means there is no template for the locale, outcome and classification. The caller blocks the explanation; nothing is guessed or borrowed from another locale.
var ErrNoTemplate = errors.New("explanation: no template")

type entry struct{ text, version string }

// Templates are the loaded pack templates: locale, then "<outcome_id>/<classification>". They also carry the pack's masking limit, the longest unmasked digit run, which
// Validate reads for check 4 (SEC-03); the number lives in the pack's masking-limits.v1 document and nowhere in code.
type Templates struct {
	byLocale map[string]map[string]entry
	masking  masking.Limits
}

// WithMaskingLimits returns the templates with the pack's masking limit. Without it, Validate blocks every text: it never uses a number of its own.
func (t Templates) WithMaskingLimits(l masking.Limits) Templates {
	t.masking = l
	return t
}

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
			if err := checkSlots(text); err != nil {
				return Templates{}, fmt.Errorf("explanation: the %s template for %s: %w", loc, key, err)
			}
			m[key] = entry{text: text, version: version}
		}
		t.byLocale[loc] = m
	}
	return t, nil
}

// LoadForbidden reads <dir>/<locale>.forbidden.json: the locale's forbidden-claim patterns of PERD 31.2 check 3, pack data next to the templates (Architect
// decision A3). The file is a JSON array of regular expressions. A missing file, a file that is not an array of strings, an empty list, an empty or blank
// pattern, a duplicate, or a pattern that does not compile is an error: a locale with no usable list cannot be validated, so it fails closed.
func LoadForbidden(dir, locale string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, locale+".forbidden.json")) // #nosec G304 -- the directory is configuration
	if err != nil {
		return nil, fmt.Errorf("explanation: forbidden-claim file for %s: %w", locale, err)
	}
	var patterns []string
	if err := json.Unmarshal(raw, &patterns); err != nil {
		return nil, fmt.Errorf("explanation: the forbidden-claim file for %s is not an array of strings: %w", locale, err)
	}
	if len(patterns) == 0 {
		return nil, fmt.Errorf("explanation: the forbidden-claim file for %s is empty", locale)
	}
	seen := map[string]bool{}
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("explanation: the forbidden-claim file for %s has an empty pattern", locale)
		}
		if seen[p] {
			return nil, fmt.Errorf("explanation: the forbidden-claim file for %s repeats %q", locale, p)
		}
		seen[p] = true
		if _, err := regexp.Compile(p); err != nil {
			return nil, fmt.Errorf("explanation: the forbidden pattern %q for %s does not compile: %w", p, locale, err)
		}
	}
	return patterns, nil
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

// Rendered is the renderer's answer: the text, what binds it, and which template and version it came from. FactIDs lists the facts that filled the template's
// slots, in slot order (empty for a template with no slot).
type Rendered struct {
	Text            string
	Claims          []Claim
	TemplateID      string
	TemplateVersion string
	FactIDs         []string
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
	text, used, err := expand(e.text, f)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Text: text, TemplateID: id, TemplateVersion: e.version, FactIDs: used, Claims: []Claim{{Span: text, TemplateID: id}}}, nil
}

var (
	sentenceEnd = regexp.MustCompile(`[.!?]+\s+|[.!?]+$`)
	figure      = regexp.MustCompile(`\p{Nd}{4}-\p{Nd}{2}-\p{Nd}{2}|\p{Nd}[\p{Nd},.:]*\p{Nd}|\p{Nd}`) // a calendar date is one value, then times and numbers
	digitRun    = regexp.MustCompile(`\p{Nd}+`)
	words       = regexp.MustCompile(`[\p{L}\p{M}][\p{L}\p{M}'’]*`)
)

// openers are the ordinary function words of English (pronouns, articles, auxiliaries), lowercase. They are language syntax, not domain data. Every other word of a
// rendered text must occur in the facts or in the pack's reviewed templates for the locale, whatever its case, so an invented person, institution or term is blocked
// (REQ-EXP-002 INVENTED_NAME). The check is deliberately strict: a false block falls back to the template, which is the safe direction.
var openers = map[string]bool{"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true, "it": true, "its": true, "we": true, "our": true,
	"your": true, "you": true, "there": true, "here": true, "if": true, "please": true, "no": true, "not": true, "and": true, "but": true, "or": true, "is": true, "are": true,
	"was": true, "were": true, "has": true, "have": true, "had": true, "will": true, "can": true, "could": true, "may": true, "about": true, "for": true, "on": true, "at": true,
	"in": true, "to": true, "of": true, "as": true, "by": true, "with": true, "do": true, "does": true, "did": true}

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

// wordSet is the lowercase words of s.
func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range words.FindAllString(s, -1) {
		set[strings.ToLower(w)] = true
	}
	return set
}

// vocabulary is every word the pack's reviewed templates for the locale use, lowercase. A fresh map each call: the caller adds the facts' words.
func (t Templates) vocabulary(locale string) map[string]bool {
	vocab := map[string]bool{}
	for _, e := range t.byLocale[locale] {
		for w := range wordSet(e.text) {
			vocab[w] = true
		}
	}
	return vocab
}

// Validate runs the four checks of PERD 31.2 and the forbidden-claim patterns (pack data, passed in). It returns every problem; none means the text may be shown.
// 1. every sentence is covered by a claim bound to a fact of the input or to the rendered template; 2. every figure, time or masked identifier in the text appears in
// the facts; 3. no forbidden pattern matches; 4. no unmasked digit run longer than the pack's masking limit (4 in the fintech pack, PERD 31.2).
func Validate(t Templates, r Rendered, f contracts.ExplanationFactsV2, forbidden []string) []string {
	var out []string
	known := map[string]bool{}
	for _, fact := range f.Facts {
		if _, dup := known[fact.FactId]; dup {
			out = append(out, fmt.Sprintf("input: fact id %q appears more than once, so its status is ambiguous", fact.FactId))
			known[fact.FactId] = false // fail closed: a duplicated id supports no claim
			continue
		}
		known[fact.FactId] = fact.FactStatus == contracts.ExplanationFactsV2FactsItemFactStatusKNOWN
	}
	want := string(f.Locale) + "/" + string(f.OutcomeId) + "/" + f.Classification
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
			if c.TemplateID != want {
				out = append(out, fmt.Sprintf("claim %d: template %q is not the one the input facts select (%q)", i, c.TemplateID, want))
				continue
			}
			text, ok := t.text(c.TemplateID)
			if !ok {
				out = append(out, fmt.Sprintf("claim %d: its span is not the text of template %q", i, c.TemplateID))
				continue
			}
			filled, _, err := expand(text, f)
			if err != nil {
				out = append(out, fmt.Sprintf("claim %d: template %q cannot be filled from the input facts: %v", i, c.TemplateID, err))
				continue
			}
			if !strings.Contains(filled, c.Span) {
				out = append(out, fmt.Sprintf("claim %d: its span is not the text of template %q filled from the input facts", i, c.TemplateID))
				continue
			}
		case len(c.FactIDs) > 0:
			bad := false
			for _, id := range c.FactIDs {
				if isKnown, present := known[id]; !present {
					out = append(out, fmt.Sprintf("claim %d: fact %q is not in the input", i, id))
					bad = true
				} else if !isKnown {
					out = append(out, fmt.Sprintf("claim %d: fact %q is not KNOWN; only KNOWN facts support a claim (INV-13)", i, id))
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
		if !allowed[normalFigure(fig)] && !coveredByTemplateOnly(t, r, f, fig) {
			out = append(out, fmt.Sprintf("check 2: %q is not a figure of the facts", fig))
		}
	}
	vocab := t.vocabulary(string(f.Locale))
	for w := range wordSet(strings.Join(factStrings(f), "\n")) {
		vocab[w] = true
	}
	for _, fact := range f.Facts {
		for w := range wordSet(fact.Kind) {
			vocab[w] = true
		}
	}
	for _, w := range words.FindAllString(r.Text, -1) {
		if lw := strings.ToLower(w); !vocab[lw] && !openers[lw] {
			out = append(out, fmt.Sprintf("check 2: the word %q is not in the facts or the pack's reviewed vocabulary (a name or an invented term)", w))
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
	if strings.ContainsAny(r.Text, "{}") {
		out = append(out, "check 2: the text holds a brace, so a placeholder was not filled or a value was injected")
	}
	if !t.masking.Loaded() {
		return append(out, "check 4: the masking limit is not loaded")
	}
	for _, run := range digitRun.FindAllString(r.Text, -1) {
		if n := utf8.RuneCountInString(run); n > t.masking.MaxUnmaskedRun() {
			out = append(out, fmt.Sprintf("check 4: an unmasked digit run of %d", n))
		}
	}
	return out
}

// coveredByTemplateOnly reports whether a figure sits only inside spans bound to the rendered template: pack text is reviewed data and not a model's claim.
func coveredByTemplateOnly(t Templates, r Rendered, f contracts.ExplanationFactsV2, fig string) bool {
	for _, c := range r.Claims {
		text, ok := t.text(c.TemplateID)
		if !ok || c.TemplateID != r.TemplateID {
			continue
		}
		if want, _, err := expand(text, f); err == nil && strings.Contains(want, c.Span) && strings.Contains(c.Span, fig) {
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

// Locales lists the locales that have templates loaded, sorted.
func (t Templates) Locales() []string {
	var out []string
	for l := range t.byLocale {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
