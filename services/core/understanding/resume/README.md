# understanding/resume

**What it does.** Resolves the conversation reference of a follow-up turn (`inv_<ULID>`, A234) to the latest admitted intent of that investigation, for the session that holds it. The result is the `Prior` of `interpreter.Input` (INP-01). **Implements:** REQ-INP-004 (a follow-up is accepted only for the investigation's own principal; another's is "not found" and raises a security event), EC-C03; INV-12 (tenant and principal come from the verified session alone).

**Order.** Incomplete resolver: a configuration error, nothing read. Invalid session or a reference that is not an investigation id: `ErrNotFound`, no read. The latest intent of `corr_<ULID>` is read as the session's tenant (`intentstore.Reader.Latest`); unknown, another tenant's and another chain all end as `ErrNotFound`. The stored document must name the session's principal, else `Denials.Denied` records a `SECURITY_EVENT` and the answer is the same `ErrNotFound` (a failure to record is named beside it). Only then is `States.Terminal` asked, and an ended investigation is `ErrTerminal` (the customer is asked for a new request, A320, A404): another principal never learns that an investigation ended.

**Not here.** The `Denials` and `States` implementations over the Record, and the call from the edge's conversation route, are slice 6 (EL reads). The follow-up's own handling is `understanding/interpreter` (`Input.Prior`).

**Tests.** `go test ./understanding/resume/` (spies, no database).
