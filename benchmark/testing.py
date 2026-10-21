"""Test support shared by the benchmark packages."""
import os


def examples(base: int) -> int:
    """Property-test example count: `base` on every merge, ten times that in the nightly deep profile (REQ-TST-002)."""
    return base * 10 if os.environ.get("NOEMA_PROPERTY_PROFILE") == "deep" else base
