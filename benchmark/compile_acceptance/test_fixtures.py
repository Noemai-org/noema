"""UND-15 (REQ-INT-002; REQ-TST-004, AGENT-12; A261; SR's conditions on noema#778, relayed): a live run's recordings become committed replay fixtures.

Replies are REDUCED (the provider's echo of the request, its tools and its opaque reasoning are dropped; the one message stays, so the reduced reply parses to the same object), requests are kept as recorded and
keyed by `request_sha256`, every fixture says SYNTHETIC, the run id, the head SHA and the A261 label, failed calls stay evidence (negative fixtures), and the tests read only committed files. All data is
SYNTHETIC (AGENT-25)."""
import hashlib
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from collections.abc import Callable
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, ClassVar

from benchmark.compile_acceptance import fixtures
from benchmark.compile_acceptance.fixtures import load_fixtures, reduce_reply, reduce_run
from benchmark.compile_acceptance.recording import Recorder, dump
from benchmark.compile_acceptance.replay import REPLAY_LABEL, RecordingsRefused, RecordingSet, ReplayRefused, ReplayStore, label_recording, load_recordings
from benchmark.compile_acceptance.test_replay import FIXTURES, PIN, answers, decide_request, slot_request

ROOT = Path(__file__).resolve().parents[2]
ALLOW = ROOT / "benchmark" / "compile_acceptance" / "leakscan.allow"
NOW = datetime(2026, 11, 19, 12, 30, tzinfo=UTC)
HEAD = "ee9156936ca1561d67040cd1764c9865471da014"  # the head of main the live run of 19 Nov 2026 was dispatched on (workflow run 44521380842)
ENCRYPTED = "gAAAAAB" + "q" * 60  # SYNTHETIC stand-in for a provider's opaque reasoning


def full_reply(text: str, *, status: str = "completed", messages: int = 1) -> bytes:
    message = {"id": "msg_0a1b", "phase": "final_answer", "role": "assistant", "status": "completed", "type": "message", "content": [{"type": "output_text", "text": text}]}
    return json.dumps({"id": "resp_0a1b", "object": "response", "created_at": 1795091423, "completed_at": 1795091426, "status": status, "model": "gpt-6-luna", "instructions": "You answer a fixed list",
                       "metadata": {}, "tools": [], "output": [{"id": "rs_0a1b", "type": "reasoning", "summary": [], "encrypted_content": ENCRYPTED}] + [message] * messages,
                       "usage": {"input_tokens": 538, "output_tokens": 141, "total_tokens": 679, "input_tokens_details": {"cached_tokens": 0}, "output_tokens_details": {"reasoning_tokens": 74}}}).encode()


def make_run(root: Path) -> Path:
    """Eight calls with full-shape replies (as the provider sends them): three answers, one answering one question of two, one not completed, three failures. Returns the `recordings` directory."""
    rec = Recorder(root / "out", run_id="compile-ghTEST", provider="openai", model_id="gpt-6-luna", now=lambda: NOW)
    url = "https://api.example.invalid/v1/responses"

    def call(case: str, request: bytes, status: int | None, reply: bytes | None, failure: str | None = None) -> None:
        rec.case_id = case
        rec.record_call(url=url, request=request, status=status, reply=reply, failure=failure, projected_micros=2000, cost_micros=100)

    call("G-A", decide_request(["intent_node"]), 200, full_reply(json.dumps(answers(["intent_node"]))))
    call("G-A", decide_request(["s0"], "one"), None, None, "UNAVAILABLE")
    call("G-A", slot_request(), 400, b'{"error": {"message": "schema refused", "type": "invalid_request_error", "code": "invalid_json_schema", "param": "text.format.schema"}}')
    call("G-A", decide_request(["f0", "f1"]), 200, full_reply(json.dumps(answers(["f0", "f1"]))))
    call("G-B", decide_request(["s0"], "two"), 500, b'{"error": {"message": "boom"}}')
    call("G-B", decide_request(["s0", "s1"]), 200, full_reply(json.dumps(answers(["s0"]))))
    call("G-C", decide_request(["s0"], "three"), 200, full_reply(json.dumps(answers(["s0"])), status="incomplete"))
    call("G-C", decide_request(["g0"]), 200, full_reply(json.dumps(answers(["g0"]))))
    rec.write_manifest(PIN)
    return root / "out" / "recordings"


Change = Callable[[Any], object]


def read(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def message_text(reply: dict[str, Any]) -> str:
    return str([o for o in reply["output"] if o["type"] == "message"][0]["content"][0]["text"])


def canonical(value: Any) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


class Reducing(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.src = make_run(self.root / "src")
        self.dst = self.root / "fixtures"
        reduce_run(self.src, self.dst, head_sha=HEAD)

    def pair(self, n: int) -> tuple[dict[str, Any], dict[str, Any]]:
        return read(self.src / f"rec-{n:04d}.json"), read(self.dst / f"rec-{n:04d}.json")

    def test_the_reduced_directory_loads_as_fixtures_with_the_same_recordings(self) -> None:
        s = load_fixtures(self.dst)
        o = load_recordings(self.src)
        self.assertEqual([(r.recording_id, r.request_sha256, r.labels) for r in s.recordings], [(r.recording_id, r.request_sha256, r.labels) for r in o.recordings])
        self.assertEqual(len(s.usable()), 3)

    def test_the_plain_loader_takes_a_reduced_directory_too(self) -> None:
        self.assertEqual(len(load_recordings(self.dst).recordings), 8)

    def test_every_request_is_kept_as_recorded_and_keyed_by_its_hash(self) -> None:
        for n in range(1, 9):
            o, d = self.pair(n)
            self.assertEqual(d["request"], o["request"], n)
            self.assertEqual(d["request_sha256"], o["request_sha256"], n)
            self.assertEqual(d["fixture"]["request_canonical_sha256"], canonical(o["request"]), n)

    def test_the_reply_of_an_answer_is_reduced_to_what_a_replay_needs(self) -> None:
        for n in (1, 4, 6, 7, 8):
            o, d = self.pair(n)
            self.assertEqual(set(d["reply"]), {"object", "status", "model", "output", "usage"}, n)
            self.assertEqual([i["type"] for i in d["reply"]["output"]], ["message"], n)
            self.assertEqual(set(d["reply"]["usage"]), {"input_tokens", "output_tokens", "total_tokens"}, n)
            self.assertNotIn("encrypted_content", json.dumps(d), n)
            self.assertLess(len(json.dumps(d["reply"])), len(json.dumps(o["reply"])), n)

    def test_the_reduced_reply_parses_to_the_same_object_and_has_the_same_labels(self) -> None:
        for n in range(1, 9):
            o, d = self.pair(n)
            self.assertEqual(label_recording(d), label_recording(o), n)
            if label_recording(o).usable_as_fixture:
                self.assertEqual(json.loads(message_text(d["reply"])), json.loads(message_text(o["reply"])), n)
                self.assertEqual(d["fixture"]["reply_message_sha256"], hashlib.sha256(message_text(o["reply"]).encode()).hexdigest(), n)

    def test_a_replay_of_a_fixture_serves_the_reduced_reply_and_a_failure_is_refused(self) -> None:
        store = ReplayStore(load_fixtures(self.dst))
        for n in range(1, 9):
            o, d = self.pair(n)
            if label_recording(o).usable_as_fixture:
                self.assertEqual(store.lookup(o["request_sha256"]), d["reply"], n)
            else:
                with self.assertRaises(ReplayRefused) as cm:
                    store.lookup(o["request_sha256"])
                self.assertEqual(cm.exception.code, "EVIDENCE_ONLY")

    def test_a_failed_call_is_kept_as_it_was_and_carries_only_the_fixture_member(self) -> None:
        for n in (2, 3, 5):
            o, d = self.pair(n)
            self.assertIsNone(d["reply"])
            self.assertEqual({k: v for k, v in d.items() if k != "fixture"}, o)
            self.assertIsNone(d["fixture"]["reply_message_sha256"])

    def test_a_reply_that_is_not_an_answer_stays_what_it_was_and_keeps_its_label(self) -> None:
        o, d = self.pair(7)  # not completed
        self.assertEqual(d["reply"]["status"], "incomplete")
        self.assertEqual(label_recording(d).reason, "NOT_COMPLETED")
        o, d = self.pair(6)  # answers one question of two
        self.assertEqual(label_recording(d).reason, "QUESTIONS_MISMATCH")

    def test_every_fixture_says_synthetic_and_names_its_run_and_head_and_the_a261_label(self) -> None:
        m = read(self.dst / "manifest.json")
        self.assertEqual(m["fixture"], {"label": REPLAY_LABEL, "synthetic": True, "run_id": "compile-ghTEST", "head_sha": HEAD, "reduced": True})
        self.assertEqual(m["label"], read(self.src / "manifest.json")["label"])  # the recorder's own label is kept
        for n in range(1, 9):
            _o, d = self.pair(n)
            f = d["fixture"]
            self.assertEqual((f["label"], f["synthetic"], f["run_id"], f["head_sha"]), (REPLAY_LABEL, True, "compile-ghTEST", HEAD), n)
            self.assertEqual(f["labels"], {"usable_as_fixture": label_recording(d).usable_as_fixture, "reason": label_recording(d).reason}, n)
            self.assertEqual(f["original_file_sha256"], hashlib.sha256((self.src / f"rec-{n:04d}.json").read_bytes()).hexdigest(), n)

    def test_the_head_must_be_a_full_lowercase_sha(self) -> None:
        for bad in ("", "abc", HEAD.upper(), HEAD[:-1], HEAD + "0", "g" * 40, " " + HEAD[1:]):
            with self.subTest(bad), self.assertRaises(RecordingsRefused) as cm:
                reduce_run(self.src, self.root / "other", head_sha=bad)
            self.assertEqual(cm.exception.code, "BAD_HEAD")
            self.assertFalse((self.root / "other").exists())

    def test_a_destination_that_holds_files_is_refused(self) -> None:
        with self.assertRaises(RecordingsRefused) as cm:
            reduce_run(self.src, self.dst, head_sha=HEAD)
        self.assertEqual(cm.exception.code, "DESTINATION_NOT_EMPTY")

    def test_a_source_the_loader_refuses_is_reduced_to_nothing(self) -> None:
        broken = self.root / "broken"
        shutil.copytree(self.src, broken)
        (broken / "rec-0004.json").unlink()
        with self.assertRaises(RecordingsRefused):
            reduce_run(broken, self.root / "none", head_sha=HEAD)
        self.assertFalse((self.root / "none").exists())

    def test_a_reduction_that_changed_the_message_is_refused_and_nothing_is_written(self) -> None:
        def changed(reply: Any) -> dict[str, Any]:
            reduced = reduce_reply(reply)
            reduced["output"][0]["content"][0]["text"] += " "
            return reduced
        original = fixtures.reduce_reply
        fixtures.reduce_reply = changed
        try:
            with self.assertRaises(RecordingsRefused) as cm:
                reduce_run(self.src, self.root / "other", head_sha=HEAD)
        finally:
            fixtures.reduce_reply = original
        self.assertEqual(cm.exception.code, "REDUCTION_CHANGED")
        self.assertFalse((self.root / "other").exists())

    def test_reduce_reply_keeps_nothing_it_was_not_told_to_keep(self) -> None:
        original = json.loads(full_reply("{}"))
        original["service_tier"] = "default"
        original["usage"]["cost"] = 5
        reduced = reduce_reply(original)
        self.assertEqual(set(reduced), {"object", "status", "model", "output", "usage"})
        self.assertEqual(reduced["usage"], {"input_tokens": 538, "output_tokens": 141, "total_tokens": 679})
        self.assertEqual(original, json.loads(json.dumps(original)))  # the argument is not changed

    def test_reduce_reply_does_not_invent_what_the_reply_lacked(self) -> None:
        self.assertEqual(reduce_reply({"status": "completed"}), {"status": "completed", "output": []})
        self.assertEqual(reduce_reply({"output": "x", "usage": 3, "model": 4}), {"output": []})


class Checking(unittest.TestCase):
    """`load_fixtures` is the strict reader of a committed fixture directory: a change to any part of it is refused."""

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.src = make_run(self.root / "src")
        self.good = self.root / "good"
        reduce_run(self.src, self.good, head_sha=HEAD)
        self.dir = self.root / "copy"
        shutil.copytree(self.good, self.dir)

    def edit(self, name: str, change: Any) -> None:
        path = self.dir / name
        doc = read(path)
        change(doc)
        path.write_text(dump(doc), encoding="utf-8")

    def refused(self, code: str = "FIXTURE_INVALID") -> None:
        with self.assertRaises(RecordingsRefused) as cm:
            load_fixtures(self.dir)
        self.assertEqual(cm.exception.code, code)

    def test_the_untouched_copy_loads(self) -> None:
        self.assertEqual(len(load_fixtures(self.dir).recordings), 8)

    def test_an_unreduced_run_is_not_a_fixture_directory(self) -> None:
        self.dir = self.src
        self.refused()

    def test_a_recording_without_the_fixture_member_is_refused(self) -> None:
        self.edit("rec-0001.json", lambda d: d.pop("fixture"))
        self.refused()

    def test_a_manifest_without_the_fixture_member_is_refused(self) -> None:
        self.edit("manifest.json", lambda m: m.pop("fixture"))
        self.refused()

    def test_the_labels_must_be_exactly_the_a261_label_and_synthetic(self) -> None:
        cases: list[tuple[str, Change]] = [("label", lambda f: f.update(label="Understanding: live output")), ("not synthetic", lambda f: f.update(synthetic=False)), ("synthetic a string", lambda f: f.update(synthetic="true")),
                             ("run id", lambda f: f.update(run_id="compile-gh1")), ("head short", lambda f: f.update(head_sha=HEAD[:39])), ("head upper", lambda f: f.update(head_sha=HEAD.upper())),
                             ("head another", lambda f: f.update(head_sha="0" * 40)), ("extra member", lambda f: f.update(note="x")), ("member missing", lambda f: f.pop("head_sha"))]
        for name, change in cases:
            with self.subTest(name):
                shutil.copy(self.good / "rec-0001.json", self.dir / "rec-0001.json")
                self.edit("rec-0001.json", lambda d: change(d["fixture"]))
                self.refused()

    def test_the_manifest_labels_are_checked_the_same_way(self) -> None:
        cases: list[tuple[str, Change]] = [("label", lambda f: f.update(label="x")), ("synthetic", lambda f: f.update(synthetic=False)), ("head", lambda f: f.update(head_sha="1" * 40)), ("reduced", lambda f: f.update(reduced=False)),
                             ("extra", lambda f: f.update(more=1))]
        for name, change in cases:
            with self.subTest(name):
                shutil.copy(self.good / "manifest.json", self.dir / "manifest.json")
                self.edit("manifest.json", lambda m: change(m["fixture"]))
                self.refused()

    def test_a_request_that_was_changed_is_refused(self) -> None:
        self.edit("rec-0001.json", lambda d: d["request"].update(input=d["request"]["input"] + " "))
        self.refused()

    def test_a_reply_whose_text_was_changed_is_refused(self) -> None:
        def change(d: dict[str, Any]) -> None:
            d["reply"]["output"][0]["content"][0]["text"] = json.dumps(answers(["intent_node"]), indent=1)
        self.edit("rec-0001.json", change)
        self.refused()

    def test_opaque_reasoning_and_the_echo_of_a_request_are_refused(self) -> None:
        cases: list[tuple[str, Change]] = [("encrypted content", lambda r: r["output"][0].update(encrypted_content=ENCRYPTED)),
                             ("a reasoning item", lambda r: r["output"].insert(0, {"id": "rs_1", "type": "reasoning", "summary": []})),
                             ("instructions", lambda r: r.update(instructions="You answer")), ("tools", lambda r: r.update(tools=[])), ("an id", lambda r: r.update(id="resp_1")),
                             ("a usage detail", lambda r: r["usage"].update(output_tokens_details={"reasoning_tokens": 1}))]
        for name, change in cases:
            with self.subTest(name):
                shutil.copy(self.good / "rec-0001.json", self.dir / "rec-0001.json")
                self.edit("rec-0001.json", lambda d: change(d["reply"]))
                self.refused()

    def test_the_recorded_labels_must_be_the_labels_the_loader_computes(self) -> None:
        self.edit("rec-0006.json", lambda d: d["fixture"]["labels"].update(usable_as_fixture=True, reason="OK"))
        self.refused()

    def test_the_hash_of_the_original_file_must_be_a_sha256(self) -> None:
        for bad in ("", "abc", "G" * 64, None, 5):
            with self.subTest(repr(bad)):
                shutil.copy(self.good / "rec-0001.json", self.dir / "rec-0001.json")
                self.edit("rec-0001.json", lambda d: d["fixture"].update(original_file_sha256=bad))
                self.refused()

    def test_a_failed_call_must_not_name_a_message_hash(self) -> None:
        self.edit("rec-0002.json", lambda d: d["fixture"].update(reply_message_sha256="0" * 64))
        self.refused()

    def test_an_answer_must_name_its_message_hash(self) -> None:
        self.edit("rec-0001.json", lambda d: d["fixture"].update(reply_message_sha256=None))
        self.refused()


class Committed(unittest.TestCase):
    """The fixtures of the live compile run of 19 Nov 2026 (workflow run 44521380842), as committed. The tests read only these files (no environment variable, no download)."""

    rset: ClassVar[RecordingSet]

    @classmethod
    def setUpClass(cls) -> None:
        cls.rset = load_fixtures(FIXTURES)

    def test_the_run_the_head_and_the_labels(self) -> None:
        m = read(FIXTURES / "manifest.json")
        self.assertEqual(m["fixture"], {"label": REPLAY_LABEL, "synthetic": True, "run_id": "compile-gh44521380842", "head_sha": HEAD, "reduced": True})
        self.assertEqual((self.rset.run_id, self.rset.model_id, self.rset.date, len(self.rset.recordings)), ("compile-gh44521380842", "gpt-6-luna", "2026-11-19", 25))
        self.assertEqual(FIXTURES.name, self.rset.run_id)

    def test_seventeen_answers_and_eight_negative_fixtures_and_no_end_to_end_claim(self) -> None:
        self.assertEqual(len(self.rset.usable()), 17)
        self.assertEqual(sorted(r.labels.reason for r in self.rset.evidence_only()), ["HTTP_4XX"] * 4 + ["NO_STATUS"] * 4)
        self.assertEqual({r.labels.step for r in self.rset.usable()}, {"harness_decide"})  # no slot-mention reply exists: the compile cannot be replayed end to end until the next run
        self.assertEqual([r.labels.step for r in self.rset.evidence_only() if r.labels.reason == "HTTP_4XX"], ["pipeline_slot_mentions"] * 4)

    def test_no_failed_call_is_served_and_every_answer_is_served_by_its_request_hash(self) -> None:
        store = ReplayStore(self.rset)
        for r in self.rset.recordings:
            doc = read(FIXTURES / f"{r.recording_id}.json")
            if r.labels.usable_as_fixture:
                self.assertEqual(store.lookup(doc["request_sha256"]), doc["reply"])
                self.assertEqual(hashlib.sha256(message_text(doc["reply"]).encode()).hexdigest(), doc["fixture"]["reply_message_sha256"])
            else:
                with self.assertRaises(ReplayRefused) as cm:
                    store.lookup(doc["request_sha256"])
                self.assertEqual(cm.exception.code, "EVIDENCE_ONLY")

    def test_no_opaque_reasoning_and_no_provider_echo_is_committed(self) -> None:
        for path in sorted(FIXTURES.iterdir()):
            text = path.read_text(encoding="utf-8")
            self.assertNotIn("encrypted_content", text, path.name)
            self.assertNotIn('"type": "reasoning"', text, path.name)
        for r in self.rset.usable():
            reply = read(FIXTURES / f"{r.recording_id}.json")["reply"]
            self.assertEqual(set(reply), {"object", "status", "model", "output", "usage"}, r.recording_id)

    def test_the_committed_directory_scans_clean_with_the_allow_file(self) -> None:
        run = subprocess.run([sys.executable, "-I", str(ROOT / "tools" / "ci" / "leakscan.py"), "--allow", str(ALLOW), str(FIXTURES)], capture_output=True, text=True, check=False)
        self.assertEqual((run.returncode, run.stdout.startswith("leakscan OK")), (0, True), run.stdout + run.stderr)

    def test_the_scan_without_the_allow_file_finds_the_named_fields_so_the_allow_lines_are_needed(self) -> None:
        run = subprocess.run([sys.executable, "-I", str(ROOT / "tools" / "ci" / "leakscan.py"), str(FIXTURES)], capture_output=True, text=True, check=False)
        self.assertEqual(run.returncode, 1)

    def test_a_planted_account_number_in_a_committed_copy_is_still_caught(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            copy = Path(tmp) / "f"
            shutil.copytree(FIXTURES, copy)
            doc = read(copy / "rec-0001.json")
            doc["reply"]["output"][0]["content"][0]["text"] += " 0123456789"
            (copy / "rec-0001.json").write_text(dump(doc), encoding="utf-8")
            run = subprocess.run([sys.executable, "-I", str(ROOT / "tools" / "ci" / "leakscan.py"), "--allow", str(ALLOW), str(copy)], capture_output=True, text=True, check=False)
            self.assertEqual(run.returncode, 1)

    def test_a_committed_file_that_was_altered_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            copy = Path(tmp) / "f"
            shutil.copytree(FIXTURES, copy)
            doc = read(copy / "rec-0001.json")
            doc["request"]["input"] += "x"
            (copy / "rec-0001.json").write_text(dump(doc), encoding="utf-8")
            with self.assertRaises(RecordingsRefused):
                load_fixtures(copy)

    def test_no_test_of_the_loader_reads_an_environment_variable_or_a_download(self) -> None:
        needle = "NOEMA_UND15_" + "RECORDINGS"
        for name in ("test_replay.py", "test_fixtures.py", "replay.py", "fixtures.py"):
            self.assertNotIn(needle, (Path(__file__).parent / name).read_text(encoding="utf-8"), name)
        self.assertEqual(self.rset.replay_label, REPLAY_LABEL)


if __name__ == "__main__":
    unittest.main()
