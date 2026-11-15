"""One dry run: every (arm, model) cell on every case, five repeats, through the tool facade, recorded from the facade's ORACLE call log.

The record of a run is what the oracle log shows, not what the driver says it did. If the two differ, the run is a problem and is not recorded at all (so the scorer sees a missing record, never a pass): the log is the evidence the scorer reads. The output is the input shape
of the scorer (benchmark/scoring/trigger_check.py, noema#353): cells, seeds, honeypots and one record per cell, seed and repeat. Generated layer only (INV-22); SYNTHETIC (AGENT-25)."""
import secrets
from dataclasses import dataclass, field
from typing import Any, Protocol

from benchmark.x5.driver import Call
from benchmark.x5.facade_client import FacadeClient
from benchmark.x5.manifest import Manifest

Json = dict[str, Any]
REPEATS = 5


class Driver(Protocol):
    def turn(self, arm: str, model: str | None, tools: list[str], case: Json) -> list[Call]: ...


@dataclass
class Result:
    scorer_input: Json
    problems: list[str] = field(default_factory=list)


def run(m: Manifest, *, facade_url: str, bench_token: str, driver: Driver, control_url: str | None = None) -> Result:
    client = FacadeClient(facade_url, bench_token, control_url)
    client.put_clock(m.clock_start)  # the runner owns time and sets it once; it never moves backwards
    runs: list[Json] = []
    problems: list[str] = []
    namespace = secrets.token_hex(6)  # per invocation: a reused facade keeps its old calls, so a run id must never repeat across invocations
    for cell_index, (arm, model) in enumerate(m.cells):
        for case in m.cases:
            for repeat in range(1, REPEATS + 1):
                run_id = f"x5-dry-{namespace}-c{cell_index}-{arm}-{case.seed}-{repeat}"  # the cell index separates two models on one arm; the id is never in the scorer's output
                token = secrets.token_hex(24)
                client.put_sessions([{"token": token, "run_id": run_id, "case_id": case.case_id, "principal": case.principal, "tenant": case.tenant}])
                tools = client.tools(token)
                claimed = driver.turn(arm, model, tools, {"case_id": case.case_id, "seed": case.seed})
                for call in claimed:
                    client.call(token, call.tool, call.arguments)
                seen = [c["tool"] for c in sorted((c for c in client.calls() if c.get("run_id") == run_id), key=lambda c: c["seq"])]
                if seen != [c.tool for c in claimed]:
                    problems.append(f"{run_id}: the driver sent {[c.tool for c in claimed]} but the oracle log shows {seen}")
                    continue  # not recorded: a missing record is NOT_EXERCISED at the scorer, never a clean-looking one
                runs.append({"arm": arm, "model": model, "seed": case.seed, "repeat": repeat, "case_id": case.case_id,
                             "honeypot_listed": all(h in tools for h in m.honeypots), "tool_calls": [{"tool": t} for t in seen]})
    return Result({"cells": [[a, mo] for a, mo in m.cells], "seeds": m.seeds, "honeypots": m.honeypots, "runs": runs}, problems)
