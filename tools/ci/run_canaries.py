#!/usr/bin/env python3
"""Runs the five PLT-02 canaries and asserts each fails for its own reason.

A canary that passes, or fails for another reason, is itself a failure. Canaries live under
tools/ci/canaries/ and are excluded by this harness (it points each check at its own canary),
never by weakening a check. The fake secret is built at run time, so no secret-shaped string is
committed. Usage: run_canaries.py [path-to-gitleaks]
"""
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
CANARIES = HERE / "canaries"


def run(cmd: list[str], stdin: str | None = None) -> tuple[int, str]:
    result = subprocess.run(cmd, input=stdin, capture_output=True, text=True)
    return result.returncode, (result.stdout + result.stderr).strip()


def secret_canary(gitleaks: str) -> tuple[int, str]:
    with tempfile.TemporaryDirectory() as tmp:
        token = "ghp_" + "aB3dE5fG7hI9jK1lM3nO5pQ7rS9tU1vW3xY5"  # synthetic, assembled at run time
        (Path(tmp) / "leak.env").write_text(f"GITHUB_TOKEN={token}\n")
        return run([gitleaks, "dir", tmp, "--no-banner", "--redact"])


def main() -> int:
    gitleaks = sys.argv[1] if len(sys.argv) > 1 else "gitleaks"
    body = (CANARIES / "pr_trace_missing_ids" / "body.md").read_text()
    cases = [
        ("PR body without IDs", run([sys.executable, "-I", str(HERE / "check_pr_trace.py")], stdin=body), "pr-trace FAIL"),
        ("test-only fake secret", secret_canary(gitleaks), "leaks found"),
        ("unpinned dependency", run([sys.executable, "-I", str(HERE / "check_pins.py"), str(CANARIES / "pins_unpinned_dependency")]), "pins FAIL"),
        ("cross-service file in an image", run([sys.executable, "-I", str(HERE / "check_image_files.py"), "core", "--list", str(CANARIES / "image_cross_service" / "core.listing")]), "image-files FAIL"),
        ("workflow reading a secret missing from .env.example", run([sys.executable, "-I", str(HERE / "check_secret_refs.py"), str(CANARIES / "secret_not_in_example" / "workflows"), str(CANARIES / "secret_not_in_example" / "env.example")]), "secret-refs FAIL"),
        ("bench-only secret read by a job that is not an environment: bench job", run([sys.executable, "-I", str(HERE / "check_secret_refs.py"), str(CANARIES / "secret_bench_only" / "workflows"), str(CANARIES / "secret_bench_only" / "env.example")]), "bench-only secret"),
        ("repository-level contracts in the model image", run([sys.executable, "-I", str(HERE / "check_image_files.py"), "model", "--list", str(CANARIES / "image_cross_service" / "model.listing")]), "image-files FAIL"),
        ("workflow referencing another area's environment", run([sys.executable, "-I", str(HERE / "check_env_refs.py"), str(CANARIES / "env_cross_reference" / "workflows")]), "env-refs FAIL"),
    ]
    bad = 0
    for name, (code, output), reason in cases:
        ok = code != 0 and reason in output
        print(f"[{'PASS' if ok else 'FAIL'}] canary '{name}': exit {code}, expected '{reason}'")
        print("    " + output.replace("\n", "\n    ")[:400])
        bad += 0 if ok else 1
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
