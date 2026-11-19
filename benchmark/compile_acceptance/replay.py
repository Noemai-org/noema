"""UND-15 (REQ-INT-002; A188, A261): STUB for the red run. The loader, the labels and the lookup of the recorded-replay provider's recordings."""
from dataclasses import dataclass
from pathlib import Path
from typing import Any

REPLAY_LABEL = "Understanding: recorded replay of live output"


class RecordingsRefused(Exception):
    def __init__(self, code: str, message: str = "") -> None:
        super().__init__(message or code)
        self.code = code


class ReplayRefused(Exception):
    def __init__(self, code: str, recording_id: str | None = None, reason: str | None = None) -> None:
        super().__init__(code)
        self.code, self.recording_id, self.reason = code, recording_id, reason


@dataclass(frozen=True, slots=True)
class Labels:
    usable_as_fixture: bool
    reason: str
    step: str
    question_ids: tuple[str, ...]
    case_id: str

    @property
    def evidence_only(self) -> bool:
        raise NotImplementedError


@dataclass(frozen=True, slots=True)
class Recording:
    recording_id: str
    file_sha256: str
    labels: Labels


@dataclass(frozen=True, slots=True)
class RecordingSet:
    run_id: str
    provider: str
    model_id: str
    date: str
    label: str
    recordings: tuple[Recording, ...]
    replay_label: str = REPLAY_LABEL

    def usable(self) -> tuple[Recording, ...]:
        raise NotImplementedError

    def evidence_only(self) -> tuple[Recording, ...]:
        raise NotImplementedError


def label_recording(doc: dict[str, Any]) -> Labels:
    raise NotImplementedError


def load_recordings(directory: Path, *, file_sha256: dict[str, str] | None = None) -> RecordingSet:
    raise NotImplementedError


class ReplayStore:
    def __init__(self, recordings: RecordingSet) -> None:
        raise NotImplementedError

    def lookup(self, request_sha256: str) -> dict[str, Any]:
        raise NotImplementedError

    def lookup_bytes(self, request: bytes) -> dict[str, Any]:
        raise NotImplementedError
