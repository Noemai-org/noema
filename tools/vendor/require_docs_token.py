#!/usr/bin/env python3
"""Fail with one clear line when the docs read token is missing (the drift guard, GOV-22).

The workflow passes the repository secret to this script in the environment variable DOCS_READ_TOKEN. An unset, empty or
blank value stops the job, so the guard can never pass without having read docs. The script never prints the value.
"""
import os
import sys

MESSAGE = "DOCS_READ_TOKEN is not set. See docs/vendors/README.md."


def main() -> int:
    if not os.environ.get("DOCS_READ_TOKEN", "").strip():
        print(MESSAGE)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
