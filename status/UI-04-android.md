# UI-04 · Android browser emulation

## Task card

```
TASK-P11-UI-04  Android browser emulation of the static overlay
Priority: P1
Requirement IDs: REQ-UI-001, POC-FR-11; SoT §18.4
Rationale: the overlay must work on a mid-range Android webview and present accessible controls.
Modules touched (allowlist): ui/**, status/UI-04-android.md
Contracts: no contract change; the static overlay does not receive backend data.
Inputs/outputs: synthetic static overlay to local Playwright browser checks.
Acceptance criteria: The Playwright suite SHALL pass for J1–J8 flows. A masking test SHALL find no digit runs longer than 4 in the DOM.
Tests to write FIRST (AGENT-11): REQ-UI-001 viewport fit and touch-token dimensions under an Android device profile; DOM privacy scan after every test.
Faults to exercise (AGENT-12): none in the static shell; this test does not replace real-connection fault cases.
Dependencies: UI-01 and UI-02 for live flows; merged static shell noema#502.
Out of scope: external browser downloads, mocked backend, real-core US-01 evidence, claiming emulation is a physical Android webview.
Definition of done: tests green; boundary/lint green; Record events asserted; docs updated; PR answers the SoT §52 questions.
If blocked or conflicting: STOP and emit the CONFLICT block (§71.3).
```

## Session 2026-11-17 · tests first
- Uses Playwright's Pixel 5 profile with the approved pre-installed desktop Chromium. This is Android browser emulation, not a physical device or Android webview.
- The tests load the built synthetic static shell and CSS locally. They check the viewport, disabled controls and the existing `--noema-touch-target` minimum in both dimensions; every flow uses the DOM privacy scanner.
- A scanner regression compares three synthetic wide-separator examples with the merged customer-answer schema pattern. It fails locally because the contract rejects the samples while the UI scanner does not. The proposed shared machine-readable A259 vector corpus is still pending; this test does not claim corpus coverage.
- CI red at `f912379b`: the Android DOM test fails because the contract rejects a sample while the scanner accepts it; the other five Playwright tests and 55 Node tests pass. The fix widens the test-only scanner to the separator categories and explicit code points in `tools/contracts/digit_run.py`. A new Node test walks every Unicode scalar as a single character joining a synthetic five-digit sequence and verifies that every sequence rejected by the schema pattern is flagged; ordinary four digits, comma and a letter remain accepted.
- Node 22.22.0 after the fix: 56 Node tests and six Playwright tests pass locally. The JavaScript runtime's newer Unicode categories can flag additional separators the schema's pinned Unicode version does not, for example U+0897; this is conservative and is not claimed as exact pattern equivalence. Browser coverage uses three representative samples, not the separate shared corpus. Physical Android webview and real-core acceptance remain outstanding.
- No network, backend or Record events are involved. The live J-flows and US-01 on a mid-range Android webview against the real core remain outstanding.


## 2026-11-23 · Android emulation sub-slice complete
- `noema#686` completes the static Pixel 5 browser-emulation slice. At `04f404df227fef14e5099f4346f46ce0a257869a`, the UI suite passed, but the core and other workflows had not all completed. At the later documentation head `b0e4d058e3a34786ec9ee5e1e453a2560637d9ff`, the UI and most workflows passed; core was still running. Viewport fit, disabled controls, touch-target dimensions, contract/scanner rejection and the DOM privacy scan are asserted; an HTTP-only request assertion proves the local fixture flow makes no API/Record writes.
- This is not physical Android or Android WebView acceptance and does **not** complete TASK-P11-UI-04. Physical-device acceptance and live J1–J8/core Record-event assertions remain deferred until after G1.
