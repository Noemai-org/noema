# tools/ci

CI safeguards (PLT-02). Standard library only, except the two pinned scanners.

| Check | Script | Fails when |
|---|---|---|
| Dependency pins | `check_pins.py` | a Go module has no `go.sum`; a Python dependency lacks `==` or hashes; an npm version is not exact or has no lock; a Docker base image lacks a digest; an Action is not a full commit SHA |
| Image file list (R2) | `check_image_files.py` | an image carries files of another service |
| Environment references (R3) | `check_env_refs.py` | a workflow references another area's GitHub Environment |
| Secret names | `check_secret_refs.py` (needs the pinned PyYAML in `requirements.lock`) | a workflow reads a secret `.env.example` does not list; a `TUSHE_ORACLE_` or `TUSHE_FIXTURE_` secret is read by anything but a job with `environment: bench` in a `bench*` workflow; a secret is looked up by an expression; the whole `secrets` context is used; `secrets: inherit`; or a file cannot be parsed. Listing a prefix in `.env.example` never grants use of it |
| PR trace | `check_pr_trace.py` | the PR body lacks a task ID, a PERD `REQ-` ID or a SoT ID |
| Secret scan | gitleaks, config `/.gitleaks.toml` | a secret is found in the history |
| SBOM | syft (CycloneDX JSON) | uploaded per image as an artifact |

`run_canaries.py` proves each check can fail. Each canary must fail for its own reason. Canaries are left out by pointing each check at its own canary, never by weakening a check. The fake secret is built at run time.

`install_tools.sh` downloads gitleaks and syft from pinned releases and verifies their SHA-256.

The `codegen-diff` job in `safeguards.yml` is disabled (`if: false`) until CON-03.
