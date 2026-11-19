# cmd/grantdrill

**What it does.** The SEC-09 key-rotation drill for the Decision Grant HMAC key, on simulated time (instants are offsets from a fixed epoch; no simulated clock object, TEN-05). `grantdrill --work DIR` (an existing, empty directory) makes three synthetic keys, writes the three key directories an operator goes through (the old key; the new key and the old; the new key alone), loads each with the core's loader (`trust/grants/keyfiles`), issues a grant every 30 simulated seconds for 90 simulated minutes, and verifies every grant still inside its 10-minute window at every tick, with a corpus of forgeries (a made-up secret under each key id; a retired key's secret after retirement; a changed field; a swapped, unknown or empty key id). It then removes the files.

**REQs.** REQ-SEC-005, INV-03, INV-25. Updated 18 Nov 2026.

**Output.** Counts only, never a key, a grant or a signature. Exit 0: 0 valid grants rejected, 0 forged grants accepted, and the control detected its mistake. Exit 1: any count above 0, too few checks, or the control saw nothing. Exit 2: it could not run (a bad argument, a work directory that is not empty, a system that cannot check key files).

**The control.** The drill also runs the unsafe rule once (the old key retired the moment the new one joins, while its grants are live) and requires valid grants to be rejected. A drill that cannot fail proves nothing.

**What it does not show.** The grant TTL, the overlap and the tick are the drill's own constants, not the deployment's configuration. It runs the loader and the key ring, not the container, its mounts or a live core: the Compose job (IL, noema#692) runs this binary in the stack's image and mounts. All keys and grants are SYNTHETIC.

**Tests.** `go test ./cmd/grantdrill/...` from `services/core/`. `drill_test.go` makes the run go wrong in four ways (the old key retired too early, never retired, a ring holding the attacker's secret, a ring that cannot be built) and requires the count that says so; `main_unix_test.go` runs the command on a real directory.
