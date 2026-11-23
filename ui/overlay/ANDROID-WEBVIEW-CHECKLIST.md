# UI-04 · physical Android webview acceptance checklist

Status: **not run**. Playwright device emulation and automated axe are not a physical Android webview result. Run only with a synthetic account and synthetic data on a real core; do not paste bearer tokens, raw responses, personal data or screenshots containing them into evidence.

## Record the test environment

- [ ] Date, tester, reviewed UI commit and real-core commit or release; link the run's non-sensitive evidence.
- [ ] Device make/model, Android version, WebView package/version, viewport and display scaling, input method, accessibility settings, and network conditions.
- [ ] Confirm the app uses the real configured core and a dedicated synthetic session; do not record credentials. Record which approved browser/device and setup was used.

## On the physical webview

- [ ] Run US-01 and the applicable J1–J8 flows against the real core; record each as pass, fail or not run with an evidence link. Observe actual server-backed progress and final answer; do not treat a local contract server as the core.
- [ ] Exercise text entry, focus order and visible focus, labels, keyboard navigation, screen-reader announcements, scaling and viewport/scroll behavior; record observed failures.
- [ ] Exercise enabled actions, including live cards and expiry where available. Verify push-to-talk is disabled when voice capture is unavailable; exercise capture and permission handling only when the approved voice path exists.
- [ ] Exercise a genuine connection interruption and recovery under the configured policy. Verify the UI refetches real state, does not show a fabricated answer, and presents safe plain-language failure if recovery fails.
- [ ] Inspect every visited UI state for never-shown material and digit-run leakage using the project's scan and approved masking rules. Record the scan result without copying the scanned content. Verify the case reference is shown only when supplied by the core.
- [ ] Run the local automated axe checks on each corresponding browser flow and record results. Automated checks do not establish WCAG 2.2 AA conformance; complete the applicable manual checks as well.
- [ ] Verify relevant Record events through the authorized audit path and retain non-sensitive evidence; the UI itself must not write Record events.

## Outcome

- [ ] Link every passing or failing flow and Record assertion; list unrun flows explicitly.
- [ ] Record owner and follow-up for each failure. Do not mark UI-04 or US-01 done until the required real-device, real-core and review evidence passes.
