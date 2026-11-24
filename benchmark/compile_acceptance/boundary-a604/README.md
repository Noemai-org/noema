# Boundary recordings of the `requested_remedy` gate (A604)

Date: 24 Nov 2026 · Owner: Benchmark Owner (role agent) · For: C2 (the gate, UND-18, noema#932) · Status: **SYNTHETIC. Scripted replies, not live model output, never to be passed off as a live recording (A261).**

## What this is

`recordings/` is a directory in the format the compile acceptance run writes (`rec-NNNN.json` and `manifest.json`), so the replay provider (`replay.py`) and the fixture reduction (`fixtures.py`) read it as they read a live run's. It holds 143 files:
the compile route run once per boundary, 19 boundaries, with an inner transport that answers from a script. No network, no key, no cost. The recorder wrote it; the model service's real pipeline, harness and provider adapter produced every request.

Each boundary changes one thing: the number the scripted "model" wrote as the probability of yes for the gate question (`requested_remedy_gate`). Each boundary is asked about a **different utterance** (the next item of the A604
labelled set, `benchmark/compile_acceptance/remedy_labelled.v1.json`), because a replay is keyed by the SHA-256 of the exact request and the gate's request is one fixed sentence: two boundaries on one utterance would be one request with two answers. The scripted answers do not depend on the label of the utterance.

`run_id` is `synthetic-boundary` in every file, and `recorded_at` is fixed (`2026-11-24T00:00:00Z`) so the output is the same on every run (INV-21; two runs were byte-identical). The manifest's label text is the recorder's own and says "recorded live output (SYNTHETIC)": read it with `run_id`; this directory is not live.

## The boundaries and what A612 expects

The expected outcome is computed from the text the provider wrote, by exact integer and fraction arithmetic and the rule of A612 (a plain decimal in [0, 1], floored to whole basis points, compared with the pack's threshold, 9000 basis points interim and a Founder decision to confirm; anything else, a missing value included, abstains). It does not read the implementation's answer. `boundary-findings.json` holds both, side by side.

| ID | Written | Expected | Why |
|---|---|---|---|
| B01 | 0.8999 | absent | one basis point below |
| B03, B04, B05 | 0.9, 0.90, 0.9000 | present | exactly the threshold, written three ways |
| B07 | 0.9001 | present | one above |
| B08 | 0.5 | absent | doubt |
| B09 | 0 | absent | zero as an integer |
| B10, B11 | 1, 1.0 | present | one |
| B13 | 1e-05 | absent | an exponent |
| B14 | 9e-1 | absent | an exponent for 0.9 |
| B15 | -0.1 | absent | a sign |
| B16, B17 | NaN, Infinity | absent | not finite numbers |
| B18, B19 | null, member missing | absent | missing |
| B20 | "0.95" (a string) | absent | a string where a number is required |
| B21 | 0.95 with "selected": "no" | absent | the model's own no |
| B22 | 0.6 with "selected": "yes" | absent | a yes at 0.6 is doubt |

**Three boundaries are not committed:** B02 (a value just under 0.9 written to five places, which floors to 8999), B06 (just over 0.9 to five places, which floors to 9000) and B12 (just over one to five places, above one by less than a basis point). Their text holds five digits in a row, which the leak rule refuses in a committed file (REQ-SEC-003, A154). Generate them with `--include-long-digit-runs`. They matter: they are the floor-never-rounds-up boundary. C2's tests cover 8999, 9000 and 9001 basis points; whether to allow these three lines in `leakscan.allow` is for SR.

## Where the gate disagrees with A612's text (findings for C2; nothing here is hidden)

At `4d2dbb72` (noema#932 head), 12 of 19 generated boundaries agree. Seven disagree, and B12 (not committed) disagrees the same way as B15 to B20:

- **B14 (`9e-1`).** The gate accepted it (present). A612 says the gate reads "the decimal the provider wrote", plain decimals only, no exponent. The provider layer hands over parsed JSON, so `9e-1` is the float 0.9 and its `repr` is `0.9`. The gate cannot tell. Either A612's "no exponent" means "an exponent that does not read back as a plain decimal" (then B14 is fine and B13 is the case), or the gate needs the raw text.
- **B15 to B20 (and B12).** A reply whose probability is a sign, NaN, infinity, null, missing, a string, or above one is refused **as a whole** by the harness's reply validation before the gate runs, and the compile service reports `ProviderUnavailable` (the call is `STOPPED`, no proposal). A612 says "a missing or malformed value abstains" (the slot stays absent). So these never reach the gate. Failing closed is safe; it differs from the text, and it costs the whole compile, not the slot. Which is intended is for the Architect and C2.

## Use, and regeneration

Load with the replay provider as any compile-run recording set. Reproduce:

```
PYTHONPATH=services/model:. python3 -I -c "import sys; from benchmark.compile_acceptance.synthetic_boundary import main; sys.exit(main(['--out', 'DIR']))"
```

It refuses when the gate (UND-18) is not on the model service's path, and exits 1 when any boundary disagrees. It was run at noema#932 head `4d2dbb72`: the request bytes depend on that code and on the pack's `slot_gates`, so after #932 changes or merges the recordings are regenerated and the SHA-256 keys change. Nothing in this directory is held-out material or an oracle (INV-22).
