# Session 2026-11-20 · ENV-09 · the hostility proxy refuses without randomness instead of panicking (Integration Lead)

## Session summary
- What changed: `requestlog.go`: `newGeneration` returns an error (`errNoRandomness`, fixed text, nothing of the source's error) instead of `panic`; `newRequestLog` and `proxy.New` return it; a reset (`PUT` plan, `DELETE` plan, `reset`) makes the new generation BEFORE changing anything and answers 500 `NO_RANDOMNESS` with nothing changed. `randRead` is the test seam.
- Why: A480 (noema#786): the PLT-09 lint baseline (forbidigo `^panic$`) finds the panic at `requestlog.go:44`, in a module the lint PR does not own; noema#786 merges once it is fixed here.
- Risks, and how they are contained: a process that cannot get randomness still cannot make a safe claim about the log; it now refuses to start or to reset instead of dying. No behaviour change when randomness works.
- Tests added (REQ-ENV-004, AGENT-12): four Go tests (error for the generation and a new log; fixed text; a refused reset changes nothing; a reset with randomness gives a new 32-character generation). They failed first (the package did not compile without the change).
- Not verified: `TestCorruptedCaseExchanges` fails on my Windows host on `main` as well (stale committed exchanges on this host); CI on Linux is the proof of the whole package.
