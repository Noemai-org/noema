"""The oracle client: the benchmark runner's one reader of ground truth (REQ-BEN-002, REQ-ENV-002). It holds the bench credential and reads the
environment after a run; it never talks to Noema, so it cannot be steered by it."""
from dataclasses import dataclass
from typing import Any

from .counters import CounterUnavailable, DeclaredAbsent, Mark, RecordGovernance, RefusalLog, Unavailable
from .effects import effects_since
from .truth import MockCaseTruth

Json = dict[str, Any]


@dataclass(frozen=True)
class Baseline:
    state: Json
    command_seq: int
    refusal_mark: Mark


class OracleClient:
    def __init__(self, truth: MockCaseTruth, *, refusal_log: RefusalLog | None, governance: RecordGovernance,
                 monetary: DeclaredAbsent | Unavailable) -> None:
        self.truth, self.refusal_log, self.governance, self.monetary = truth, refusal_log, governance, monetary

    def _refusals(self) -> RefusalLog:
        if self.refusal_log is None:
            raise CounterUnavailable(RefusalLog.name, "no mock-case log is configured")
        return self.refusal_log

    def baseline(self) -> Baseline:
        """Taken after the fixture is loaded and before the first input: everything seeded so far is not an effect of the case."""
        mark = self._refusals().mark()
        commands = self.truth.commands()
        return Baseline(self.truth.state(), commands[-1]["seq"] if commands else 0, mark)

    def readout(self, baseline: Baseline, record_events: list[Json] | None) -> Json:
        """The `environment` block of a case result (benchmark-case-result.v1.json)."""
        accesses = self._refusals().since(baseline.refusal_mark)
        governance = self.governance.count(record_events)
        monetary = self.monetary.value()
        after = self.truth.state()
        commands = self.truth.commands(after_seq=baseline.command_seq)
        return {"effects": effects_since(baseline.state, after, commands), "oracle_accesses_by_runtime": accesses,
                "model_governance_changes": governance, "monetary_calls": monetary}

    def missing_sources(self) -> list[str]:
        """The counters that have no source, as `name: reason`. A run cannot start while any is missing."""
        return [f"{name}: {text.removeprefix('unavailable: ')}" for name, text in self.sources().items() if text.startswith("unavailable")]

    def sources(self) -> dict[str, str]:
        """Where each counter comes from, for the run manifest."""
        return {
            RefusalLog.name: self.refusal_log.describe() if self.refusal_log else "unavailable: no mock-case log is configured",
            RecordGovernance.name: self.governance.describe(),
            "monetary_calls": self.monetary.describe(),
        }
