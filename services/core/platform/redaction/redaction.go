// Package redaction is the mandatory redactor of the core (SEC-03; REQ-SEC-003, REQ-OBS-002; PERD §44.2; AGENT-13; ruling A154).
//
// The rule is the PERD's: the longest run of digits that reaches an outward surface (a log line or attribute, a trace attribute, an API or error body, explanation text, a served fact,
// a model prompt, the UI feed) is four. MaskAccountNumbers masks every run longer than four digits to asterisks plus its last four. A digit is any Unicode decimal digit, so a number
// written in full-width, Arabic-Indic or Devanagari digits is masked like an ASCII one. A run is digits with any number of separators between two digits; a comma ends a run,
// so an amount written "20,000,000" (groups of three) is left alone. The characters that join the digits of a run, and any number of them between two digits, are the Record guard's (record/payloadguard; A194, A231, A289, A259): see separators.
//
// There is no threshold to configure. The only exceptions are the typed exemptions listed by Exemptions: typed IDs, RFC 3339 timestamps, and structured numeric fields (minor-unit money,
// counts, sizes, durations). They are decided by type, never by length or by how a value looks in free text. A string-valued field is always strict: ten digits under the key
// "amount_minor" are masked. The list is an AGENT-16 constant; changing it needs a Founder's approval and a decision record, and a test fails if it changes without its tests.
//
// The redactor also removes the value under a secret-looking key, inside a secret-looking group, and inside any map, struct or slice it cannot read safely. A string value is scanned for the
// credential of an Authorization line, a bearer token and a "password=..." style pair, whatever key it sits under (scrubSecrets); this is a small scan of known forms, not a guarantee.
//
// May import: the standard library.
//
// Must never: pass a secret, a full account number or raw audio through unmasked (AGENT-13).
package redaction

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Redacted replaces the value under a secret-looking key.
const Redacted = "[REDACTED]"

// MaxUnmaskedRun is the longest digit run that stays visible (PERD §44.2): it is also the number of trailing digits a masked run keeps.
const MaxUnmaskedRun = 4

// Exemption is one thing the redactor leaves alone, by type.
type Exemption struct {
	Name string
	Rule string
}

// Exemptions is the list of typed exemptions (A154). It is an AGENT-16 constant: a change needs a Founder's approval, and redaction_test.go fails when it differs from the tests.
func Exemptions() []Exemption {
	return []Exemption{
		{"typed_id", "a whole token of the shape <prefix>_<ULID>: two to six lowercase letters, an underscore, then 26 Crockford base32 characters, the first 0 to 7, at least one a letter; not touching a letter, digit or underscore"},
		{"rfc3339_timestamp", "a whole token that parses as an RFC 3339 date-time with at most nine fraction digits (a date alone is not one); not touching a letter, digit, underscore or hyphen"},
		{"minor_money_field", "an integer or float attribute whose key ends in _minor"},
		{"count_field", "an integer or float attribute whose key ends in _count"},
		{"size_field", "an integer or float attribute whose key ends in _bytes"},
		{"duration_field", "a time.Duration attribute, or an integer or float attribute whose key ends in _ms, _s, _seconds, _minutes or _micros"},
	}
}

// The traversal bounds of a value the redactor reads member by member (a map or a slice): nesting deeper than maxDepth, or a value that makes it visit more than maxNodes members, is
// replaced by [REDACTED]. They stop a cycle, a very deep value and a shared sub-structure that a plain walk would expand without end; they are not safety thresholds.
const (
	maxDepth = 8
	maxNodes = 4096
)

// numericSuffixes are the key suffixes of the structured numeric exemptions: the key is chosen by code, never by a user, a model or an enterprise field.
var numericSuffixes = []string{"_minor", "_count", "_bytes", "_ms", "_s", "_seconds", "_minutes", "_micros"}

// separators is the class of characters that join the digits of a run: the one definition of the Record's append guard (record/payloadguard, A194, A231), adopted here by ruling A289 (A259:
// one separator definition on every surface). Every kind of space (\s, \p{Z}, the vertical tab, the next-line character), every dash (\p{Pd}) and the minus sign, every bracket (\p{Ps},
// \p{Pe}), the dot, the slash, the colon and the underscore with their full-width forms and the middle dots, every format character (\p{Cf}: zero-width characters, soft hyphen, joiners) and
// every combining mark (\p{M}); any NUMBER of them between two digits. A comma is not in it. It is a copy, not an import: record/ is a P0 path and platform/ imports the standard library
// only; TestEveryCodePointJoinsDigitsExactlyWhenTheRecordGuardSaysSo asks both about every code point and fails when they differ.
const separators = `\s\x0b\x{85}\p{Z}\p{Cf}\p{M}\p{Pd}\p{Ps}\p{Pe}.\-/_:\x{2212}\x{b7}\x{30fb}\x{ff0e}\x{ff0f}\x{ff1a}\x{ff3f}`

var (
	runPattern = regexp.MustCompile(`\p{Nd}(?:[` + separators + `]*\p{Nd})*`)
	// secretPattern is the list of key words whose value is removed (substring, any case). "pin" is not in it: as a substring it would remove "shipping" and "mapping", so it is a word (pinKey).
	secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|auth|token|secret|password|passwd|pwd|credential|bearer|cookie|private[_-]?key|signing[_-]?key|raw[_-]?audio)`)
	// pinKey is the word "pin" in a key: alone, between non-letters (user_pin, new-pin, pin2) or at a camel-case boundary (userPin, userPIN, PINCode). Not inside a word (shipping, pinned).
	pinKey = regexp.MustCompile(`(?:^|[^A-Za-z])(?i:pin)(?:[^a-z]|$)|[a-z](?:Pin|PIN)(?:[^a-z]|$)`)
	// bearerValue and secretValue find a secret inside a string value, whatever the key it sits under: an Authorization header line, a bearer token, a "key=value" or "key: value" pair
	// whose key is one of the secret words (a URL query, a JSON body, a header dump). They are small on purpose: only the key word directly before the colon or equals sign, or "bearer"
	// directly before a token of eight characters or more, so ordinary prose ("password reset", "max_tokens=100", "the bearer of news") is left alone.
	bearerValue = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=\-]{8,}`)
	secretValue = regexp.MustCompile(`(?i)((?:^|[^a-z])pin|api[_-]?key|authorization|token|secret|passw(?:or)?d|pwd|credential|cookie|(?:private|signing)[_-]?key)["']?\s*[:=]\s*(?:bearer\s+|basic\s+)?("[^"]*"|'[^']*'|[^\s,;&"']+)`)
	typedIDRe   = regexp.MustCompile(`[a-z]{2,6}_[0-7][0-9A-HJKMNP-TV-Z]{25}`)
	timestampRe = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-][0-9]{2}:[0-9]{2})`)
)

// Redactor masks and removes. It holds no state and is safe for concurrent use.
type Redactor struct{}

// New returns the redactor. There is nothing to configure (A154).
func New() *Redactor { return &Redactor{} }

// MaskAccountNumbers replaces every digit run longer than four digits with asterisks plus its last four, except inside a typed ID or an RFC 3339 timestamp.
func (r *Redactor) MaskAccountNumbers(s string) string {
	spans := exemptSpans(s)
	if len(spans) == 0 {
		return maskRuns(s)
	}
	var b strings.Builder
	prev := 0
	for _, sp := range spans {
		b.WriteString(maskRuns(s[prev:sp[0]]))
		b.WriteString(s[sp[0]:sp[1]])
		prev = sp[1]
	}
	b.WriteString(maskRuns(s[prev:]))
	return b.String()
}

func maskRuns(s string) string {
	return runPattern.ReplaceAllStringFunc(s, func(run string) string {
		digits := make([]rune, 0, len(run))
		for _, c := range run {
			if unicode.IsDigit(c) {
				digits = append(digits, c)
			}
		}
		if len(digits) <= MaxUnmaskedRun {
			return run
		}
		return strings.Repeat("*", len(digits)-MaxUnmaskedRun) + string(digits[len(digits)-MaxUnmaskedRun:])
	})
}

// exemptSpans returns the byte ranges of the typed IDs and timestamps in s, in order and without overlap.
func exemptSpans(s string) [][2]int {
	var spans [][2]int
	for _, loc := range typedIDRe.FindAllStringIndex(s, -1) {
		if touches(s, loc, false) || !hasLetter(s[loc[0]+strings.IndexByte(s[loc[0]:loc[1]], '_')+1:loc[1]]) {
			continue
		}
		spans = append(spans, [2]int{loc[0], loc[1]})
	}
	for _, loc := range timestampRe.FindAllStringIndex(s, -1) {
		if touches(s, loc, true) {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, s[loc[0]:loc[1]]); err != nil {
			continue
		}
		spans = append(spans, [2]int{loc[0], loc[1]})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	out := spans[:0]
	end := -1
	for _, sp := range spans {
		if sp[0] >= end {
			out = append(out, sp)
			end = sp[1]
		}
	}
	return out
}

// touches is true when the character before or after the span is a letter, a digit or an underscore (or, for a timestamp, a hyphen): the span is part of a longer token.
func touches(s string, loc []int, hyphen bool) bool {
	bad := func(c rune) bool {
		return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || (hyphen && c == '-')
	}
	if loc[0] > 0 {
		if c, _ := utf8.DecodeLastRuneInString(s[:loc[0]]); bad(c) {
			return true
		}
	}
	if loc[1] < len(s) {
		if c, _ := utf8.DecodeRuneInString(s[loc[1]:]); bad(c) {
			return true
		}
	}
	return false
}

func hasLetter(s string) bool {
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

func secretKey(key string) bool { return secretPattern.MatchString(key) || pinKey.MatchString(key) }

// scrubSecrets removes the secret inside a string value: the credential of an Authorization line, a bearer token, the value of a "password=..." or "api_key: ..." pair.
func scrubSecrets(s string) string {
	s = bearerValue.ReplaceAllString(s, "${1} "+Redacted)
	return secretValue.ReplaceAllString(s, "${1}="+Redacted)
}

// text is what a string VALUE goes through before it leaves: its secrets removed, then its digit runs masked. Keys and group names are masked only (they are names, classified by the
// key rules, not scanned for secrets).
func (r *Redactor) text(s string) string { return r.MaskAccountNumbers(scrubSecrets(s)) }

func numericKey(key string) bool {
	for _, suf := range numericSuffixes {
		if strings.HasSuffix(key, suf) {
			return true
		}
	}
	return false
}

// FilterAttributes is the trace attribute filter: it returns a copy with long digit runs masked and the value under a secret-looking key removed. Every value is a string, so the
// numeric exemptions never apply here.
func (r *Redactor) FilterAttributes(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		if secretKey(k) {
			out[r.MaskAccountNumbers(k)] = Redacted
			continue
		}
		out[r.MaskAccountNumbers(k)] = r.text(v)
	}
	return out
}

// Handler wraps inner so that no record reaches it unmasked: the message, every attribute (groups recursively, errors, stringers, numbers, maps, slices) and every attribute added with
// With. Everything under a secret-looking group opened with WithGroup is removed. A value it cannot read safely (a struct, a pointer, a byte slice) is replaced by [REDACTED], never
// passed through (fail closed).
func (r *Redactor) Handler(inner slog.Handler) slog.Handler { return &handler{r: r, inner: inner} }

type handler struct {
	r      *Redactor
	inner  slog.Handler
	secret bool // a group opened with WithGroup has a secret-looking name: every value under it is removed
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.text(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.redactAttr(a, h.secret))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.r.redactAttr(a, h.secret)
	}
	return &handler{r: h.r, inner: h.inner.WithAttrs(red), secret: h.secret}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &handler{r: h.r, inner: h.inner.WithGroup(h.r.MaskAccountNumbers(name)), secret: h.secret || secretKey(name)}
}

// redactAttr redacts one attribute. force is true inside a secret-looking group: every value under it is removed and only the shape (keys and groups) stays.
func (r *Redactor) redactAttr(a slog.Attr, force bool) slog.Attr {
	budget := maxNodes
	return r.redactNode(a, force, 0, &budget)
}

func (r *Redactor) redactNode(a slog.Attr, force bool, depth int, budget *int) slog.Attr {
	*budget--
	if depth > maxDepth || *budget < 0 {
		return slog.String(r.MaskAccountNumbers(a.Key), Redacted)
	}
	out := r.redactValue(a, force, depth, budget)
	out.Key = r.MaskAccountNumbers(a.Key) // the key is classified by its original name above and emitted masked: an account number can be a key or a group name
	return out
}

func (r *Redactor) redactValue(a slog.Attr, force bool, depth int, budget *int) slog.Attr {
	v := a.Value.Resolve()
	if secretKey(a.Key) && !(force && v.Kind() == slog.KindGroup) {
		return slog.String(a.Key, Redacted)
	}
	if v.Kind() == slog.KindGroup {
		group := v.Group()
		if len(group) > *budget {
			return slog.String(a.Key, Redacted) // wider than the budget left: replaced once, never expanded member by member (Codex P2 on e095b99)
		}
		out := make([]any, len(group))
		for i, g := range group {
			out[i] = r.redactNode(g, force, depth+1, budget)
		}
		return slog.Group(a.Key, out...)
	}
	if force {
		return slog.String(a.Key, Redacted)
	}
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, r.text(v.String()))
	case slog.KindBool, slog.KindDuration:
		return slog.Attr{Key: a.Key, Value: v} // a duration is a duration whatever its key (the duration_field exemption); a bool holds no digits
	case slog.KindInt64, slog.KindUint64, slog.KindFloat64:
		if numericKey(a.Key) {
			return slog.Attr{Key: a.Key, Value: v}
		}
		return slog.String(a.Key, r.text(v.String()))
	case slog.KindTime:
		return slog.String(a.Key, r.MaskAccountNumbers(v.Time().UTC().Format(time.RFC3339Nano)))
	default:
		return r.redactAny(a.Key, v.Any(), force, depth, budget)
	}
}

// redactAny reads a value of kind Any: an error or a Stringer by its text, a map or slice of plain values member by member, a named scalar by its text, and anything else as [REDACTED].
func (r *Redactor) redactAny(key string, x any, force bool, depth int, budget *int) slog.Attr {
	switch t := x.(type) {
	case nil:
		return slog.String(key, "<nil>")
	case error:
		return slog.String(key, r.text(fmt.Sprint(t)))
	case fmt.Stringer:
		return slog.String(key, r.text(fmt.Sprint(t)))
	case map[string]string:
		if len(t) > *budget {
			return slog.String(key, Redacted)
		}
		attrs := make([]any, 0, len(t))
		for _, k := range sortedKeys(t) {
			attrs = append(attrs, slog.String(k, t[k]))
		}
		return r.redactNode(slog.Group(key, attrs...), force, depth+1, budget)
	case map[string]any:
		if len(t) > *budget {
			return slog.String(key, Redacted)
		}
		attrs := make([]any, 0, len(t))
		for _, k := range sortedKeys(t) {
			attrs = append(attrs, slog.Any(k, t[k]))
		}
		return r.redactNode(slog.Group(key, attrs...), force, depth+1, budget)
	case []string:
		if len(t) > *budget {
			return slog.String(key, Redacted)
		}
		attrs := make([]any, len(t))
		for i, s := range t {
			attrs[i] = slog.String(fmt.Sprint(i), s)
		}
		return r.redactNode(slog.Group(key, attrs...), force, depth+1, budget)
	case []any:
		if len(t) > *budget {
			return slog.String(key, Redacted)
		}
		attrs := make([]any, len(t))
		for i, e := range t {
			attrs[i] = slog.Any(fmt.Sprint(i), e)
		}
		return r.redactNode(slog.Group(key, attrs...), force, depth+1, budget)
	}
	switch reflect.ValueOf(x).Kind() {
	case reflect.String, reflect.Bool:
		return slog.String(key, r.text(fmt.Sprint(x)))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if numericKey(key) {
			return slog.Int64(key, reflect.ValueOf(x).Int()) // a primitive: the type's own MarshalJSON is never forwarded
		}
		return slog.String(key, r.text(fmt.Sprint(x)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if numericKey(key) {
			return slog.Uint64(key, reflect.ValueOf(x).Uint())
		}
		return slog.String(key, r.text(fmt.Sprint(x)))
	case reflect.Float32, reflect.Float64:
		if numericKey(key) {
			return slog.Float64(key, reflect.ValueOf(x).Float())
		}
		return slog.String(key, r.text(fmt.Sprint(x)))
	}
	return slog.String(key, Redacted) // a struct, a pointer, a byte slice, a channel, a function: not readable safely
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
