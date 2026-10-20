"""The mandatory redactor: masks account data to the last four digits before any log line or provider call.

Implements: REQ-SEC-003, REQ-OBS-002.

May import: the standard library.

Must never: pass a secret, a full account number or raw audio through unmasked (AGENT-13).

Created by T1 of DEC-2026-10-20 (PERD v1.11 section 64.2). An empty boundary package; nothing is built here yet.
"""
