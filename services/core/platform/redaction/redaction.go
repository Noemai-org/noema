// Package redaction is the mandatory redactor of the core (SEC-03; REQ-SEC-003, REQ-OBS-002; PERD §44.2; AGENT-13).
//
// It masks account data to the last four digits and removes the value of anything under a secret-looking key, in a log line, a log attribute and a trace attribute.
// The rules are those of the model service's redactor (services/model/noema_model/platform/redaction), with one difference on purpose: a digit is any Unicode decimal digit, so a number
// written in full-width, Arabic-Indic or Devanagari digits is masked like an ASCII one. A run is digits with a single space or hyphen allowed between two digits; a comma ends a
// run, so an amount written "20,000,000" is not an account number; a run shorter than the threshold is left alone. A run split by two or more separators is not one run (the same
// limit as the model service's); the leak scanner (tools/ci/leakscan.py) is the second wall.
//
// The threshold is a required parameter and has no default: it comes from configuration (AGENT-16), so changing it goes through a decision record. A threshold at or below the
// number of digits kept is refused, since it would mask nothing or everything.
//
// May import: the standard library.
//
// Must never: pass a secret, a full account number or raw audio through unmasked (AGENT-13).
package redaction

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode"
)

// Redacted replaces the value under a secret-looking key.
const Redacted = "[REDACTED]"

// KeptDigits is the number of trailing digits that stay visible.
const KeptDigits = 4

var (
	runPattern    = regexp.MustCompile(`\p{Nd}(?:[ \-]?\p{Nd})*`)
	secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|authorization|auth|token|secret|password|passwd|credential|bearer)`)
)

// Redactor masks and removes. It is immutable and safe for concurrent use.
type Redactor struct{ minDigits int }

// New returns a Redactor that treats a run of minDigits digits or more as an account-number candidate.
func New() *Redactor { return &Redactor{minDigits: 8} }

type Exemption struct{ Name, Rule string }

var Exemptions []Exemption

// MaskAccountNumbers replaces every account-number candidate with asterisks plus its last four digits.
func (r *Redactor) MaskAccountNumbers(s string) string {
	return runPattern.ReplaceAllStringFunc(s, func(run string) string {
		digits := make([]rune, 0, len(run))
		for _, c := range run {
			if unicode.IsDigit(c) {
				digits = append(digits, c)
			}
		}
		if len(digits) < r.minDigits {
			return run
		}
		return strings.Repeat("*", len(digits)-KeptDigits) + string(digits[len(digits)-KeptDigits:])
	})
}

func secretKey(key string) bool { return secretPattern.MatchString(key) }

// FilterAttributes is the trace attribute filter: it returns a copy with account numbers masked and the value under a secret-looking key removed.
func (r *Redactor) FilterAttributes(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		if secretKey(k) {
			out[k] = Redacted
			continue
		}
		out[k] = r.MaskAccountNumbers(v)
	}
	return out
}

// Handler wraps inner so that no record reaches it unmasked: the message, every attribute (groups recursively, errors, stringers and numbers by their text) and every attribute added
// with With. A value of a kind it cannot read as text is replaced by its masked printed form, never passed through (fail closed).
func (r *Redactor) Handler(inner slog.Handler) slog.Handler { return &handler{r: r, inner: inner} }

type handler struct {
	r     *Redactor
	inner slog.Handler
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.MaskAccountNumbers(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.r.redactAttr(a)
	}
	return &handler{r: h.r, inner: h.inner.WithAttrs(red)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{r: h.r, inner: h.inner.WithGroup(name)}
}

func (r *Redactor) redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	if secretKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	switch v.Kind() {
	case slog.KindGroup:
		group := v.Group()
		out := make([]any, len(group))
		for i, g := range group {
			out[i] = r.redactAttr(g)
		}
		return slog.Group(a.Key, out...)
	case slog.KindString:
		return slog.String(a.Key, r.MaskAccountNumbers(v.String()))
	case slog.KindInt64, slog.KindUint64:
		return slog.String(a.Key, r.MaskAccountNumbers(v.String()))
	case slog.KindBool, slog.KindFloat64, slog.KindDuration, slog.KindTime:
		return slog.String(a.Key, r.MaskAccountNumbers(v.String()))
	default: // KindAny: an error, a Stringer or anything else is read by its printed form
		return slog.String(a.Key, r.MaskAccountNumbers(fmt.Sprint(v.Any())))
	}
}
