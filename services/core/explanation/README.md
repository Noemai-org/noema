# services/core/explanation

**What it does.** The deterministic side of REQ-EXP-002 (rulings A8, A18): loads the pack's explanation templates as data, renders the fallback text for a locale, outcome and classification, and runs the PERD 31.2 claim validator on any rendered text (the fallback's or, later, a model's). No model, no network, no clock.

**REQs.** REQ-EXP-002; INV-13, AGENT-17, AGENT-05 (nothing in trust, execution or outcome imports this package).

**Rules.** A missing locale, key or empty text fails closed (`ErrNoTemplate`); there is no silent fallback to another locale here (that is a register decision, REQ-EXP-003). `Validate` returns every problem: each sentence needs a claim bound to a fact of the input or to the rendered template (and a template claim must quote that template's own text); every figure, time or masked identifier must appear in the facts; forbidden-claim patterns (pack data, passed in) must not match; no digit run longer than 4.

**Not here.** The model renderer and the HTTP route (model service, REQ-EXP-001); other locales (EXP-04); a forbidden-pattern list as pack data (not yet in the pack: `Validate` takes the patterns as an argument and the PR notes this).

**Tests.** `go test ./explanation` from `services/core/`.
