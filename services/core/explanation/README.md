# services/core/explanation

**What it does.** The deterministic side of REQ-EXP-002 (rulings A8, A18): loads the pack's explanation templates as data, renders the fallback text for a locale, outcome and classification, and runs the PERD 31.2 claim validator on any rendered text (the fallback's or, later, a model's). No model, no network, no clock.

**REQs.** REQ-EXP-002; INV-13, AGENT-17, AGENT-05 (nothing in trust, execution or outcome imports this package).

**Rules.** A missing locale, key or empty text fails closed (`ErrNoTemplate`); there is no silent fallback to another locale here (that is a register decision, REQ-EXP-003). `Validate` returns every problem: each sentence needs a claim bound to a fact of the input or to the rendered template (and a template claim must quote that template's own text); every figure, time or masked identifier must appear in the facts, and every word must come from the facts, the pack's reviewed templates for the locale, or ordinary function words (so an invented name is caught whatever its case); forbidden-claim patterns (pack data, passed in) must not match; no digit run longer than 4.

**Not here.** The model renderer and the HTTP route (model service, REQ-EXP-001); other locales (EXP-04); a forbidden-pattern list as pack data (not yet in the pack: `Validate` takes the patterns as an argument and the PR notes this).

**Tests.** `go test ./explanation` from `services/core/`.

**Forbidden-claim lists.** `LoadForbidden(dir, locale)` reads `<dir>/<locale>.forbidden.json` (pack data, Architect decision A3) and returns the patterns to pass to `Validate`. It fails closed on a missing file, an empty list, a blank, repeated or non-compiling pattern.

**What the validator does not prove (EL's review of noema#224).** A claim bound to a fact needs only that the fact exist and be KNOWN: the binding is not semantic, so any in-vocabulary sentence could sit under any KNOWN fact. Check 2 reads digits and letters in every script (`\p{Nd}`, `\p{L}`, `\p{M}`), so a number in fullwidth or Arabic-Indic digits or a name in another script is checked like its ASCII form. `Fallback` trusts the `outcome_id` and `classification` it is given: a future caller must take them from the Outcome verdict, never from model output. `ErrNoTemplate` is a bare sentinel whose message carries internal keys: never show it to a customer. `Validate` is about 100 lines (over the 60-line review trigger) because the four checks share one pass over the same rendered text; splitting it would copy that pass four times.

**Check 4 (digit runs).** The longest unmasked run is the pack's `masking-limits.v1` value (4), read through `platform/masking` and attached with `Templates.WithMaskingLimits`. Templates without it block every text (`check 4: the masking limit is not loaded`); the validator has no number of its own.
