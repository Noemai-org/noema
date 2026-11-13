"""SEC-03 (A154 as corrected; A162, option A; REQ-SEC-003, AGENT-13, AGENT-16): the harness has one source for the longest unmasked digit run, the pack's
masking-limits.v1 (value 4), the same file the core reads. The harness configuration no longer carries a number of its own: a config with or without the old
`masking` member loads, the member is ignored, and what reaches a provider is masked at the shared limit. All data is SYNTHETIC."""

import json
from pathlib import Path

import pytest

from noema_model.harness import Harness, assemble_harness, load_harness_config
from noema_model.harness.assemble import ENV_CONFIG, ENV_LLM_POOL, ENV_MASKING_LIMITS
from noema_model.platform.masking import MaskingLimits
from noema_model.platform.telemetry import InMemoryTelemetry
from tests.support import ManualClock, ScriptedProvider, make_config, raw_config, run
from tests.test_harness_call import good, request

LIMITS = MaskingLimits(4)
LIMITS_DOC = {"schema_version": "1.0", "pack_id": "fintech", "label": "SYNTHETIC", "max_unmasked_digit_run": 4}


def harness(masking_member: object | None = "absent") -> tuple[Harness, ScriptedProvider]:
    raw = raw_config(providers=("alpha",))
    if masking_member == "absent":
        del raw["masking"]
    else:
        raw["masking"] = masking_member
    provider = ScriptedProvider("alpha", [good("alpha")])
    return Harness(load_harness_config(raw), {"alpha": provider}, ManualClock(), InMemoryTelemetry(), masking=LIMITS), provider


def test_a_config_without_the_masking_member_loads() -> None:
    raw = raw_config(providers=("alpha",))
    del raw["masking"]
    config = load_harness_config(raw)
    assert not hasattr(config, "account_run_min_digits")


def test_the_old_masking_member_is_ignored_and_never_decides_anything() -> None:
    config = make_config(providers=("alpha",))  # raw_config still carries {"masking": {"account_run_min_digits": 8}}
    assert not hasattr(config, "account_run_min_digits")
    with_member = raw_config(providers=("alpha",))
    without = raw_config(providers=("alpha",))
    del without["masking"]
    assert load_harness_config(with_member) == load_harness_config(without)


@pytest.mark.parametrize("member", ["absent", {"account_run_min_digits": 8}, {"account_run_min_digits": 5}])
def test_a_five_digit_run_is_masked_whatever_the_config_says(member: object) -> None:
    """The floor of 8 in the frozen configs let a 5 to 8 digit run through to a provider (A154: nothing longer than 4 leaves unmasked)."""
    h, provider = harness(member)
    run(h.generate(request({"utterance": "my pin is 12345 and my card ends 123456789"})))
    prompt = provider.calls[0].user
    assert "12345" not in prompt and "*2345" in prompt
    assert "123456789" not in prompt and "*****6789" in prompt


def test_a_four_digit_run_is_left_alone() -> None:
    h, provider = harness()
    run(h.generate(request({"utterance": "the last four are 6789"})))
    assert "6789" in provider.calls[0].user


def test_the_harness_cannot_be_built_without_the_shared_limits() -> None:
    config = make_config(providers=("alpha",))
    provider = ScriptedProvider("alpha", [good("alpha")])
    with pytest.raises(TypeError):
        Harness(config, {"alpha": provider}, ManualClock(), InMemoryTelemetry())  # type: ignore[call-arg]


def env(tmp_path: Path, limits: object = LIMITS_DOC, member: object = "absent") -> dict[str, str]:
    raw = raw_config(providers=("alpha",))
    if member == "absent":
        del raw["masking"]
    else:
        raw["masking"] = member
    cfg = tmp_path / "harness.json"
    cfg.write_text(json.dumps(raw))
    lim = tmp_path / "masking_limits.json"
    lim.write_text(limits if isinstance(limits, str) else json.dumps(limits))
    return {ENV_CONFIG: str(cfg), ENV_LLM_POOL: "2", ENV_MASKING_LIMITS: str(lim)}


@pytest.mark.parametrize("member", ["absent", {"account_run_min_digits": 8}])
def test_the_service_builds_its_harness_with_the_shared_limits_file(tmp_path: Path, member: object) -> None:
    from tests.support import RecordingTransport
    from tests.test_assemble import chat_reply
    from tests.test_harness_call import version

    transport = RecordingTransport(chat_reply(version("alpha")))
    result = assemble_harness(env(tmp_path, member=member), ManualClock(), InMemoryTelemetry(), transport)
    assert isinstance(result.harness, Harness) and result.reason == ""
    run(result.harness.generate(request({"utterance": "pin 12345"})))
    sent = json.dumps(transport.requests[0][2])
    assert "12345" not in sent and "*2345" in sent


def test_without_the_limits_setting_there_is_no_harness_and_the_reason_names_it(tmp_path: Path) -> None:
    e = env(tmp_path)
    del e[ENV_MASKING_LIMITS]
    result = assemble_harness(e, ManualClock(), InMemoryTelemetry(), None)
    assert result.harness is None and ENV_MASKING_LIMITS in result.reason


@pytest.mark.parametrize(
    "limits",
    ["", "not json", "[]", {**LIMITS_DOC, "max_unmasked_digit_run": 8}, {**LIMITS_DOC, "max_unmasked_digit_run": 3}, {**LIMITS_DOC, "extra": 1}, {"schema_version": "1.0"}],
)
def test_a_limits_file_that_is_not_the_contract_leaves_no_harness_and_no_value_in_the_reason(tmp_path: Path, limits: object) -> None:
    result = assemble_harness(env(tmp_path, limits=limits), ManualClock(), InMemoryTelemetry(), None)
    assert result.harness is None and "masking limits" in result.reason
    assert "not json" not in result.reason and "pack_id" not in result.reason  # the reason names the file's fault, never its content


def test_an_unreadable_limits_file_leaves_no_harness(tmp_path: Path) -> None:
    e = env(tmp_path)
    e[ENV_MASKING_LIMITS] = str(tmp_path / "absent.json")
    result = assemble_harness(e, ManualClock(), InMemoryTelemetry(), None)
    assert result.harness is None and "masking" in result.reason
