# record/payloadguard

The write-side guard of the Record (A184, A193, A194, A201) and the one definition of a run of digits.

- **What it does.** `Check` refuses a payload whose strings hold a run of more than `Limit` (4) digits, with typed exemptions (A193). `Mask` keeps only the last `Limit` digits of every run of a string; `record/reconstruct` serves its facts through it and keeps no regexp of its own (A194).
- **One definition, two modes.** `baseSeparators` is the one set that joins digits (spaces, dots, hyphens, dashes, slashes, every format character and combining mark). The guard (`digitRun`) uses it as is, because a code such as `VERSION_CONFLICT_409_412` is not a number and a false refusal stops a write. The display mask (`displayRun`) also joins across an underscore, a bracket and a colon, because a number split that way is still the number to a reader. The mask is the stricter of the two, so a masked string never holds a run the guard refuses (a property test).
- **The limit** equals the one number of `masking-limits.v1` (`TestTheGuardsLimitIsTheMaskingLimitOfThePack`).
- **REQs.** REQ-AUD-003, REQ-SEC-003; A154, A184, A193, A194, A201.
- **Tests.** `go test ./record/payloadguard/ ./record/reconstruct/`.
