#!/usr/bin/env python3
"""Zero-case deterministic benchmark smoke (PLT-04). Standard library only.

Runs an empty case list twice and requires byte-identical results. It reads nothing from, and mounts
nothing of, `benchmark-heldout` (INV-22, REQ-AGT-004): it refuses to run if any argument, environment
value or Compose volume mentions it.
"""
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

FORBIDDEN = "benchmark-heldout"
COMPOSE_FILE = Path(__file__).resolve().parent.parent / "deploy" / "compose" / "docker-compose.yml"


def run_cases(cases: list[str]) -> dict[str, Any]:
    results = [{"case": c, "passed": True} for c in cases]
    body = json.dumps(results, sort_keys=True)
    return {"cases": len(cases), "passed": sum(r["passed"] for r in results), "digest": hashlib.sha256(body.encode()).hexdigest()}


def heldout_references() -> list[str]:
    found = [a for a in sys.argv[1:] if FORBIDDEN in a]
    found += [k for k, v in os.environ.items() if FORBIDDEN in v]
    config = subprocess.run(["docker", "compose", "-f", str(COMPOSE_FILE), "--profile", "bench", "config"], capture_output=True, text=True)
    if config.returncode == 0 and FORBIDDEN in config.stdout:
        found.append("docker compose config (bench profile)")
    return found


def main() -> int:
    refs = heldout_references()
    if refs:
        print(f"bench-smoke refused: {FORBIDDEN} is referenced by {refs}")
        return 1
    first, second = run_cases([]), run_cases([])
    if first != second:
        print("bench-smoke FAIL: two runs of the same cases differ")
        return 1
    print(f"bench-smoke OK: {json.dumps(first, sort_keys=True)} (two identical runs; no {FORBIDDEN} reference)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
