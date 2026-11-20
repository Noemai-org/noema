"""UND-16 (A361, A372; the Architect's answer 5, relayed; REQ-INT-001, REQ-BEN-004; INV-22, AGENT-25): the record-only pass of the compile acceptance run.

After the golden examples the run takes the inputs of BO's `benchmark/replay/g1_inputs.json` (five generated cases, inputs only) through the same compile pipeline and records every call with the same
recorder, so UND-15 can later make replay fixtures of them. No expectation is compared or invented: the run's verdict does not depend on what the model answers for them, a failure of one is recorded and
not hidden, and the goldens come first and cannot be lost to it. The file's own `inputs_sha256` is checked and the run refuses to start on any mismatch or any key beyond the closed shape.

The files here are small SYNTHETIC copies of the real file's shape, built by the test; the real file is noema#804's and is not read by these tests. The compile function in the loop tests is a scripted stand-in
that proves the loop, not any model (the same exception as test_runner.py); the budget stop is the real `BudgetGuard`; the recorder is the real one."""
import hashlib
import json
import re
import tempfile
import unittest
from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.golden import GOLDEN, RECEIVED_AT, canonical_input as golden_input
from benchmark.compile_acceptance.record_only import DEFAULT_PATH, ROOT, RecordOnlyInput, RecordOnlyRefused, canonical_input, load_inputs
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.runner import CompileFailed, run_cases, write_run_record
from benchmark.compile_acceptance.test_cli import KEY, argv, run_main
from benchmark.compile_acceptance.test_golden import good

SECRET_TEXT = "Wetin happen to the N2,500 wey I send Chidi dis morning?"
SUITE = {"generator": "noema-casegen", "generator_version": "1.3.3", "manifest_sha256": "sha256:" + "a" * 64, "seed": 20261020, "tree_sha256": "sha256:" + "b" * 64}


def canonical_bytes(doc: Mapping[str, Any]) -> bytes:
    """The rule of BO's exporter (`benchmark/replay/g1_inputs.py`): the document without its digest, sorted keys, indent 2, not ASCII-escaped."""
    return json.dumps(doc, indent=2, sort_keys=True, ensure_ascii=False).encode("utf-8")


def case(case_id: str, text: str, locale: list[str], channel: str = "text") -> dict[str, Any]:
    return {"case_id": case_id, "case_sha256": "sha256:" + "c" * 64, "channel": channel, "locale_mix": locale, "utterances": [{"turn": 1, "text": text}]}


def five() -> list[dict[str, Any]]:
    return [case("BEN-CLEAN-0001", SECRET_TEXT, ["pcm"]), case("BEN-CLEAN-0014", "Where is the 20k I sent to Ibrahim this morning?", ["en-NG"]),
            case("BEN-CLEAN-0027", "Abeg where is the 30k wey I send Amina dis morning?", ["pcm", "en-NG"]), case("BEN-TMO-0001", "Wetin happen to the 10k wey I send Bola dis morning?", ["pcm"]),
            case("BEN-TMO-0018", "Where is the N50,000 I sent to Bola this morning?", ["en-NG"])]


def make_doc(cases: list[dict[str, Any]] | None = None) -> dict[str, Any]:
    doc: dict[str, Any] = {"label": "SYNTHETIC generated cases, inputs only", "suite": dict(SUITE), "cases": five() if cases is None else cases}
    doc["inputs_sha256"] = "sha256:" + hashlib.sha256(canonical_bytes(doc)).hexdigest()
    return doc


def rehash(doc: dict[str, Any]) -> dict[str, Any]:
    body = {k: v for k, v in doc.items() if k != "inputs_sha256"}
    doc["inputs_sha256"] = "sha256:" + hashlib.sha256(canonical_bytes(body)).hexdigest()
    return doc


def write(tmp: str, doc: Mapping[str, Any] | bytes, name: str = "g1_inputs.json") -> Path:
    path = Path(tmp) / name
    path.write_bytes(doc if isinstance(doc, bytes) else canonical_bytes(doc) + b"\n")
    return path


def refused(path: Path) -> str:
    try:
        load_inputs(path)
    except RecordOnlyRefused as err:
        return err.code
    raise AssertionError("the file was accepted")


class Loading(unittest.TestCase):
    def test_a_well_formed_file_loads_in_order_with_the_inputs_only(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            got = load_inputs(write(tmp, make_doc()))
            self.assertEqual([(i.case_id, i.channel, i.locale_mix) for i in got],
                             [("BEN-CLEAN-0001", "text", ("pcm",)), ("BEN-CLEAN-0014", "text", ("en-NG",)), ("BEN-CLEAN-0027", "text", ("pcm", "en-NG")), ("BEN-TMO-0001", "text", ("pcm",)), ("BEN-TMO-0018", "text", ("en-NG",))])
            self.assertEqual(got[0].text, SECRET_TEXT)
            self.assertTrue(all(isinstance(i, RecordOnlyInput) for i in got))

    def test_the_inputs_digest_must_match_the_content(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            doc = make_doc()
            doc["cases"][0]["utterances"][0]["text"] = "Where is my money?"  # edited after the digest was made
            self.assertEqual(refused(write(tmp, doc)), "DIGEST")
            doc = make_doc()
            doc["inputs_sha256"] = "sha256:" + "0" * 64
            self.assertEqual(refused(write(tmp, doc)), "DIGEST")
            doc = make_doc()
            doc["inputs_sha256"] = "not a digest"
            self.assertEqual(refused(write(tmp, doc)), "SHAPE")

    def test_a_key_beyond_the_closed_shape_is_refused_even_when_the_digest_matches(self) -> None:
        # INV-22: nothing but inputs. A file that carries an expectation, an oracle reference, a category, a fault plan, a goal or a principal is not an inputs file, however well it is hashed.
        for where, key in [("top", "expected"), ("top", "oracle_ref"), ("suite", "category"), ("case", "expected"), ("case", "oracle_ref"), ("case", "category"), ("case", "fault_plan"), ("case", "goal"),
                           ("case", "principal"), ("utterance", "expected"), ("utterance", "intent")]:
            with self.subTest(where=where, key=key), tempfile.TemporaryDirectory() as tmp:
                doc = make_doc()
                target = {"top": doc, "suite": doc["suite"], "case": doc["cases"][2], "utterance": doc["cases"][2]["utterances"][0]}[where]
                target[key] = "x"
                self.assertEqual(refused(write(tmp, rehash(doc))), "SHAPE")

    def test_a_missing_key_is_refused(self) -> None:
        for where, key in [("top", "cases"), ("top", "suite"), ("top", "label"), ("top", "inputs_sha256"), ("suite", "seed"), ("suite", "tree_sha256"), ("case", "case_id"), ("case", "case_sha256"), ("case", "channel"),
                           ("case", "locale_mix"), ("case", "utterances"), ("utterance", "turn"), ("utterance", "text")]:
            with self.subTest(where=where, key=key), tempfile.TemporaryDirectory() as tmp:
                doc = make_doc()
                target = {"top": doc, "suite": doc["suite"], "case": doc["cases"][0], "utterance": doc["cases"][0]["utterances"][0]}[where]
                del target[key]
                self.assertEqual(refused(write(tmp, rehash(doc) if key != "inputs_sha256" else doc)), "SHAPE")

    def test_a_value_of_the_wrong_kind_or_range_is_refused(self) -> None:
        def with_case(**over: Any) -> list[dict[str, Any]]:
            c = case("BEN-CLEAN-0001", "Where is my transfer?", ["en-NG"])
            c.update(over)
            return [c]

        bad: dict[str, list[dict[str, Any]]] = {
            "no cases": [], "voice channel": with_case(channel="voice"), "unknown channel": with_case(channel="sms"), "empty locale mix": with_case(locale_mix=[]), "locale not a list": with_case(locale_mix="pcm"),
            "locale entry not text": with_case(locale_mix=[1]), "locale entry empty": with_case(locale_mix=[""]), "no utterance": with_case(utterances=[]), "two utterances": with_case(utterances=[
                {"turn": 1, "text": "a"}, {"turn": 2, "text": "b"}]), "empty text": with_case(utterances=[{"turn": 1, "text": ""}]), "blank text": with_case(utterances=[{"turn": 1, "text": "   "}]),
            "text not text": with_case(utterances=[{"turn": 1, "text": 7}]), "text with an edge space": with_case(utterances=[{"turn": 1, "text": " a"}]),
            "text not in NFC": with_case(utterances=[{"turn": 1, "text": "e\u0301"}]), "text too long": with_case(utterances=[{"turn": 1, "text": "x" * 2001}]), "turn is a bool": with_case(utterances=[{"turn": True, "text": "a"}]),
            "turn is text": with_case(utterances=[{"turn": "1", "text": "a"}]), "case id shape": with_case(case_id="ben-clean-1"), "case id with a digit run": with_case(case_id="BEN-CLEAN-00012"),
            "duplicate case ids": with_case() + with_case(), "case digest shape": with_case(case_sha256="nope"), "too many cases": [case(f"BEN-CLEAN-{n:04d}", "a", ["en-NG"]) for n in range(1, 22)],
        }
        for name, cases in bad.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                self.assertIn(refused(write(tmp, make_doc(cases))), {"SHAPE"})

    def test_a_file_that_is_not_strict_json_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            good_bytes = canonical_bytes(make_doc()) + b"\n"
            for name, raw in {"empty": b"", "not json": b"{", "array": b"[]", "nan": good_bytes.replace(b'"seed": 20261020', b'"seed": NaN'), "not utf-8": b"\xff\xfe{}",
                              "duplicate key": good_bytes.replace(b'"seed": 20261020,', b'"seed": 1,\n    "seed": 20261020,')}.items():
                with self.subTest(name):
                    self.assertEqual(refused(write(tmp, raw)), "NOT_JSON")

    def test_a_missing_file_a_directory_a_symlink_and_a_huge_file_are_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(refused(Path(tmp) / "absent.json"), "MISSING")
            self.assertEqual(refused(Path(tmp)), "MISSING")
            real = write(tmp, make_doc())
            link = Path(tmp) / "link.json"
            link.symlink_to(real)
            self.assertEqual(refused(link), "MISSING")
            self.assertEqual(refused(write(tmp, b"{" + b" " * (1 << 20) + b"}", "big.json")), "SIZE")

    def test_a_refusal_names_no_utterance_and_no_key_of_the_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            doc = make_doc()
            doc["cases"][0]["expected"] = {"secret": "x"}
            try:
                load_inputs(write(tmp, rehash(doc)))
            except RecordOnlyRefused as err:
                self.assertEqual(str(err), err.code)
                self.assertNotIn("Chidi", str(err))
                self.assertNotIn("expected", str(err))
            else:
                self.fail("accepted")

    def test_the_default_file_is_the_one_the_benchmark_owner_publishes(self) -> None:
        self.assertEqual(DEFAULT_PATH, ROOT / "benchmark" / "replay" / "g1_inputs.json")


class Derived(unittest.TestCase):
    def inputs(self) -> tuple[RecordOnlyInput, ...]:
        with tempfile.TemporaryDirectory() as tmp:
            return load_inputs(write(tmp, make_doc()))

    def test_the_canonical_input_has_the_goldens_shape_and_carries_the_utterance_only(self) -> None:
        golden = golden_input(GOLDEN[0])
        for inp in self.inputs():
            doc = canonical_input(inp)
            self.assertEqual(set(doc), set(golden))
            self.assertEqual((doc["text"], doc["channel"], doc["mode"], doc["received_at"], doc["tenant_id"], doc["principal_ref"], doc["schema_version"]),
                             (inp.text, "text", golden["mode"], RECEIVED_AT, golden["tenant_id"], golden["principal_ref"], "2.0"))
            self.assertNotIn("locale_hint", doc)  # the pipeline detects the locale itself, as for the goldens; the generator's locale mix is not sent

    def test_the_ids_are_derived_from_the_case_id_and_match_the_contracts_patterns(self) -> None:
        common = json.loads((ROOT / "contracts" / "schemas" / "common.v1.json").read_text())["$defs"]
        request_rx, corr_rx = re.compile(common["ClientRequestId"]["pattern"]), re.compile(common["CorrelationId"]["pattern"])
        seen: set[str] = set()
        for inp in self.inputs():
            doc = canonical_input(inp)
            self.assertEqual(doc, canonical_input(inp))  # the same every time (INV-21)
            self.assertIsNotNone(request_rx.fullmatch(doc["request_id"]))
            self.assertIsNotNone(corr_rx.fullmatch(doc["correlation_id"]))
            self.assertRegex(doc["request_id"], r"[a-f]")  # never an all-digit id
            self.assertNotRegex(doc["correlation_id"], r"[0-9]")  # letters only, so no id carries a digit run (AGENT-13)
            self.assertNotIn(doc["request_id"], {g.request_id for g in GOLDEN})
            seen.add(doc["request_id"])
            seen.add(doc["correlation_id"])
        self.assertEqual(len(seen), 10)


class Script:
    """A scripted compile for the record-only pass: it spends from the guard like the transport would, records one call under the recorder's case id, and answers with whatever it is told."""

    def __init__(self, guard: BudgetGuard, recorder: Recorder, *, spend: int = 1_000, fail: frozenset[str] = frozenset(), bug: frozenset[str] = frozenset(), answer: Mapping[str, Any] | None = None) -> None:
        self.guard, self.recorder, self.spend, self.fail, self.bug, self.answer, self.calls = guard, recorder, spend, fail, bug, answer, 0
        self.order: list[str] = []
        self.attempted: list[str] = []
        self.seen_case_ids: list[str] = []

    def _call(self, case_id: str) -> None:
        self.attempted.append(case_id)
        self.guard.authorize(self.spend)  # the guard decides before a call is made, as in the transport
        self.order.append(case_id)
        self.guard.settle(self.spend, self.spend)
        self.seen_case_ids.append(self.recorder.case_id)
        self.recorder.record_call(url="https://h.invalid/v1/responses", request=b'{"input":"x"}', status=200, reply=b'{"model":"m"}', failure=None, projected_micros=self.spend, cost_micros=self.spend)
        self.calls += 1

    async def golden(self, g: Any) -> Mapping[str, Any]:
        self._call(g.case_id)
        return good(g.case_id)

    async def record_only(self, inp: RecordOnlyInput) -> Mapping[str, Any]:
        self.recorder.case_id = inp.case_id
        self._call(inp.case_id)
        if inp.case_id in self.bug:
            raise RuntimeError(f"a bug that holds the utterance: {inp.text}")
        if inp.case_id in self.fail:
            raise CompileFailed("no usable reply")
        return {"intent_node": "nonsense", "slots": 3} if self.answer is None else self.answer


def recorder(root: Path) -> Recorder:
    return Recorder(root / "out", run_id="r1", provider="openai", model_id="gpt-6-luna", now=lambda: datetime(2026, 11, 17, tzinfo=UTC))


def loaded() -> tuple[RecordOnlyInput, ...]:
    with tempfile.TemporaryDirectory() as tmp:
        return load_inputs(write(tmp, make_doc()))


IDS = ["BEN-CLEAN-0001", "BEN-CLEAN-0014", "BEN-CLEAN-0027", "BEN-TMO-0001", "BEN-TMO-0018"]


class Loop(unittest.IsolatedAsyncioTestCase):
    async def run_all(self, tmp: str, guard: BudgetGuard, script: Script, rec: Recorder) -> Any:
        return await run_cases(GOLDEN, script.golden, guard, rec, calls=lambda: script.calls, record_only=loaded(), record_one=script.record_only)

    async def test_the_record_only_cases_run_after_every_golden_in_order_under_their_own_case_ids(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec)
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual(s.order, [g.case_id for g in GOLDEN] + IDS)
            self.assertEqual([(r.case_id, r.status) for r in result.record_only], [(i, "RECORDED") for i in IDS])
            self.assertEqual(s.seen_case_ids[len(GOLDEN):], IDS)  # each call is recorded under the record-only case's id
            self.assertEqual((result.calls, result.spent_micros, result.stopped, result.exit_code), (10, 10_000, False, 0))
            rec.write_manifest({})
            manifest = json.loads((Path(tmp) / "out" / "recordings" / "manifest.json").read_text())
            self.assertEqual([r["case_id"] for r in manifest["recordings"]][len(GOLDEN):], IDS)

    async def test_no_expectation_is_read_so_whatever_the_model_answers_the_verdict_is_the_goldens(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec, answer={})
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual([r.status for r in result.record_only], ["RECORDED"] * 5)
            self.assertEqual([c.status for c in result.cases], ["MET"] * 5)
            self.assertEqual(result.exit_code, 0)

    async def test_a_compile_that_failed_is_recorded_as_an_error_and_changes_neither_the_goldens_nor_the_exit_code(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec, fail=frozenset({"BEN-CLEAN-0014", "BEN-TMO-0001"}))
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual([r.status for r in result.record_only], ["RECORDED", "ERROR", "RECORDED", "ERROR", "RECORDED"])
            self.assertEqual([c.status for c in result.cases], ["MET"] * 5)
            self.assertEqual(result.exit_code, 0)
            self.assertTrue(all(r.detail for r in result.record_only if r.status == "ERROR"))

    async def test_an_unexpected_exception_is_recorded_by_type_only_and_the_goldens_and_the_pass_survive(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec, bug=frozenset({"BEN-CLEAN-0001", "BEN-TMO-0018"}))
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual([r.status for r in result.record_only], ["ERROR", "RECORDED", "RECORDED", "RECORDED", "ERROR"])
            self.assertEqual(result.record_only[0].detail, "unexpected: RuntimeError")  # not hidden; the message (it held the utterance) is not kept (AGENT-13)
            self.assertEqual([c.status for c in result.cases], ["MET"] * 5)
            self.assertEqual(result.exit_code, 0)

    async def test_a_golden_that_missed_does_not_skip_the_pass_and_the_run_stays_exit_one(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec)

            async def golden(g: Any) -> Mapping[str, Any]:
                s._call(g.case_id)
                return {"intent_node": "fin.meta.out_of_scope", "slots": {}} if g.case_id == "G-B" else good(g.case_id)

            result = await run_cases(GOLDEN, golden, guard, rec, calls=lambda: s.calls, record_only=loaded(), record_one=s.record_only)
            self.assertEqual([c.status for c in result.cases], ["MET", "MISSED", "MET", "MET", "MET"])
            self.assertEqual([r.status for r in result.record_only], ["RECORDED"] * 5)
            self.assertEqual(result.exit_code, 1)

    async def test_a_stop_during_the_goldens_leaves_every_record_only_case_not_run_and_makes_no_call_for_them(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=2_500)
            rec = recorder(Path(tmp))
            s = Script(guard, rec, spend=1_000)
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual([c.status for c in result.cases], ["MET", "MET", "NOT_RUN", "NOT_RUN", "NOT_RUN"])
            self.assertEqual([(r.case_id, r.status) for r in result.record_only], [(i, "NOT_RUN") for i in IDS])
            self.assertEqual((result.calls, result.stopped, result.exit_code), (2, True, 1))
            self.assertEqual(s.order, ["G-A", "G-B"])  # the third golden is refused by the guard before its call is made
            self.assertFalse(set(IDS) & set(s.attempted))  # not even attempted: the stop was already there

    async def test_a_stop_during_the_pass_is_kept_and_the_rest_are_not_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=7_500)  # five goldens and two record-only calls at 1,000 each, then the stop
            rec = recorder(Path(tmp))
            s = Script(guard, rec, spend=1_000)
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual([r.status for r in result.record_only], ["RECORDED", "RECORDED", "NOT_RUN", "NOT_RUN", "NOT_RUN"])
            self.assertEqual([c.status for c in result.cases], ["MET"] * 5)
            self.assertEqual((result.stopped, result.exit_code, result.calls), (True, 1, 7))

    async def test_a_stop_at_the_last_input_is_kept(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=9_500)  # nine calls fit, the tenth is refused
            rec = recorder(Path(tmp))
            s = Script(guard, rec, spend=1_000)
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual([r.status for r in result.record_only], ["RECORDED"] * 4 + ["NOT_RUN"])
            self.assertEqual((result.stopped, result.exit_code), (True, 1))

    async def test_the_guard_counts_every_record_only_call(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000, spent_micros=326_693)
            rec = recorder(Path(tmp))
            s = Script(guard, rec, spend=2_000)
            result = await self.run_all(tmp, guard, s, rec)
            self.assertEqual(result.spent_micros, 326_693 + 10 * 2_000)

    async def test_without_record_only_cases_the_loop_is_what_it_was(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec)
            result = await run_cases(GOLDEN, s.golden, guard, rec, calls=lambda: s.calls)
            self.assertEqual((result.record_only, result.calls, result.exit_code), ([], 5, 0))

    async def test_a_record_only_pass_without_a_function_to_run_it_is_a_programming_error_not_a_silent_skip(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec)
            with self.assertRaises(ValueError):
                await run_cases(GOLDEN, s.golden, guard, rec, calls=lambda: s.calls, record_only=loaded())

    async def test_the_run_record_names_the_record_only_cases_apart_from_the_goldens_and_holds_no_utterance(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            rec = recorder(Path(tmp))
            s = Script(guard, rec, fail=frozenset({"BEN-TMO-0001"}), bug=frozenset({"BEN-CLEAN-0001"}))
            result = await self.run_all(tmp, guard, s, rec)
            path = write_run_record(Path(tmp) / "out", "r1", result, {"hard_stop_micros": 5_000_000, "prior_spent_micros": 0})
            doc = json.loads(path.read_text())
            self.assertEqual([c["case_id"] for c in doc["cases"]], [g.case_id for g in GOLDEN])  # the goldens' list is the goldens only
            self.assertEqual(doc["record_only_count"], 5)
            self.assertEqual([(r["case_id"], r["status"]) for r in doc["record_only"]], [("BEN-CLEAN-0001", "ERROR"), ("BEN-CLEAN-0014", "RECORDED"), ("BEN-CLEAN-0027", "RECORDED"), ("BEN-TMO-0001", "ERROR"), ("BEN-TMO-0018", "RECORDED")])
            text = path.read_text()
            self.assertNotIn("Chidi", text)
            self.assertNotIn("Ibrahim", text)
            self.assertNotIn("N50,000", text)


class Command(unittest.TestCase):
    def test_a_run_with_the_file_records_the_pass_names_it_in_the_summary_and_the_run_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write(tmp, make_doc())
            code, out, _, built = run_main(argv(tmp, record_only=path), {"OPENAI_API_KEY": KEY})
            self.assertEqual(code, 0)
            self.assertEqual(built[0].calls(), 10)
            run = json.loads((Path(tmp) / "out" / "run.json").read_text())
            self.assertEqual((run["record_only_count"], [r["case_id"] for r in run["record_only"]], run["calls"]), (5, IDS, 10))
            self.assertEqual(len(run["cases"]), 5)
            for case_id in IDS:
                self.assertIn(f"{case_id} RECORDED", out)
            self.assertRegex(out, r"record-only: 5")
            manifest = json.loads((Path(tmp) / "out" / "recordings" / "manifest.json").read_text())
            self.assertEqual([r["case_id"] for r in manifest["recordings"]][5:], IDS)
            self.assertEqual(manifest["calls"], 10)
            self.assertEqual(set(manifest), {"run_id", "provider", "model_id", "date", "calls", "pin", "failure_classes", "reported_model_ids", "label", "recordings"})  # the manifest's shape is UND-15's and is not changed here

    def test_a_failure_of_a_record_only_case_is_shown_and_does_not_change_the_exit_code(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write(tmp, make_doc())
            code, out, _, _ = run_main(argv(tmp, record_only=path), {"OPENAI_API_KEY": KEY}, fail_record_only=True)
            self.assertEqual(code, 0)
            self.assertEqual(out.count(" ERROR"), 5)
            run = json.loads((Path(tmp) / "out" / "run.json").read_text())
            self.assertEqual({r["status"] for r in run["record_only"]}, {"ERROR"})

    def test_a_golden_failure_is_still_exit_one_with_the_pass_present(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, _, _ = run_main(argv(tmp, record_only=write(tmp, make_doc())), {"OPENAI_API_KEY": KEY}, fail=True)
            self.assertEqual(code, 1)

    def test_the_goldens_only_flag_runs_no_record_only_case(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, out, _, built = run_main(argv(tmp), {"OPENAI_API_KEY": KEY})
            self.assertEqual((code, built[0].calls()), (0, 5))
            run = json.loads((Path(tmp) / "out" / "run.json").read_text())
            self.assertEqual((run["record_only_count"], run["record_only"]), (0, []))
            self.assertNotIn("BEN-", out)

    def test_a_bad_file_is_exit_two_and_builds_nothing_and_writes_nothing(self) -> None:
        for name in ("digest", "key", "missing"):
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                doc = make_doc()
                if name == "digest":
                    doc["cases"][0]["utterances"][0]["text"] = "changed"
                elif name == "key":
                    doc["cases"][0]["expected"] = "x"
                    rehash(doc)
                path = Path(tmp) / "absent.json" if name == "missing" else write(tmp, doc)
                code, out, err, built = run_main(argv(tmp, record_only=path), {"OPENAI_API_KEY": KEY})
                self.assertEqual((code, built), (2, []))
                self.assertFalse((Path(tmp) / "out").exists())
                self.assertNotIn("Chidi", out + err)

    def test_the_default_run_asks_for_the_default_file_and_refuses_when_it_is_not_there(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = argv(tmp)
            base.remove("--skip-record-only")
            code, _, err, built = run_main(base + ["--record-only-inputs", str(Path(tmp) / "nope.json")], {"OPENAI_API_KEY": KEY})
            self.assertEqual((code, built), (2, []))
            self.assertIn("record-only", err)

    def test_a_dry_run_prices_the_record_only_cases_too_and_still_never_exits_zero(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, out, _, built = run_main(argv(tmp, dry=True, record_only=write(tmp, make_doc())), {})
            self.assertEqual(code, 3)
            for case_id in IDS:
                self.assertIn(f"  {case_id}: 555", out)
            self.assertEqual(built[0].calls(), 0)

    def test_a_dry_run_with_a_bad_file_is_exit_two(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, _, built = run_main(argv(tmp, dry=True, record_only=Path(tmp) / "absent.json"), {})
            self.assertEqual((code, built), (2, []))

    def test_the_key_is_in_no_file_and_no_line_of_output_with_the_pass(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            key = KEY
            _, out, err, _ = run_main(argv(tmp, record_only=write(tmp, make_doc())), {"OPENAI_API_KEY": key})
            self.assertNotIn(key, out + err)
            for p in (Path(tmp) / "out").rglob("*"):
                if p.is_file():
                    self.assertNotIn(key, p.read_text())


if __name__ == "__main__":
    unittest.main()
