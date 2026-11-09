# tools/ci

CI safeguards (PLT-02). Standard library only, except the two pinned scanners.

| Check | Script | Fails when |
|---|---|---|
| Dependency pins | `check_pins.py` | a Go module has no `go.sum`; a Python dependency lacks `==` or hashes; an npm version is not exact or has no lock; a Docker base image lacks a digest; an Action is not a full commit SHA |
| Image file list (R2) | `check_image_files.py` | an image carries files of another service |
| Environment references (R3) | `check_env_refs.py` | a workflow references another area's GitHub Environment |
| Secret names | `check_secret_refs.py` | a workflow reads `secrets.NAME` and `.env.example` does not list `NAME` (a trailing `*` covers a prefix; `GITHUB_TOKEN` is built in; a secret looked up by an expression fails) |
| Leak scanner (SEC-03) | `leakscan.py` | run on logs, traces, API output, stored text, explanations or Record dumps: digit runs of 5 or more, card numbers, secrets, or a seeded canary (REQ-SEC-003). Never prints a value. Not yet run over a benchmark run; SEC-07 wires that |
| Tushe administrator credential | `check_no_admin_in_workflows.py` | any YAML under `.github/` names the Tushe administrator secret or Environment, in any case or separator spelling, comments included (SEC-02, OD-16) |
| PR trace | `check_pr_trace.py` | the PR body lacks a task ID, a PERD `REQ-` ID or a SoT ID |
| Secret scan | gitleaks via `gitleaks_range.py`, config `/.gitleaks.toml` | a secret is found in the commits of this change: a pull request's `base..head`, or what a push adds (not every ref; the range is printed). `test_gitleaks_range.py` runs it against the real gitleaks |
| SBOM | syft (CycloneDX JSON) | uploaded per image as an artifact |

`run_canaries.py` proves each check can fail. Each canary must fail for its own reason. Canaries are left out by pointing each check at its own canary, never by weakening a check. The fake secret is built at run time.

`install_tools.sh` downloads gitleaks and syft from pinned releases and verifies their SHA-256.

The `codegen-diff` job in `safeguards.yml` is disabled (`if: false`) until CON-03.

**Not enforced yet: oracle and fixture secrets only in `bench`.** No oracle or fixture credential has been issued, so `.env.example` lists none and the check lets no job read one. When the credential is issued, add its name to `.env.example` and the bench-only rule (a job-aware check that reads the workflow with a YAML parser and allows the secret only in a job with `environment: bench`) in the same pull request. That version was built and tested in commit `803e027` of noema#29 and is in its history.
