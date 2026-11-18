"""UND-16 (noema#546, A198): the recorder. One file per provider call, with the request hash the replay provider will key on (UND-15, A261), the provider, the model pin, the run id and the date; the
credential and the host are never written. SYNTHETIC inputs only (AGENT-25)."""
import hashlib
import json
import tempfile
import unittest
from datetime import UTC, datetime
from pathlib import Path

from benchmark.compile_acceptance.recording import Recorder

NOW = datetime(2026, 11, 17, 9, 30, tzinfo=UTC)
REQUEST = json.dumps({"model": "gpt-6-luna", "input": "Wetin happen to the 20k wey I send Tunde this morning?"}).encode()
REPLY = json.dumps({"id": "resp_abc", "model": "gpt-6-luna-2026-10-01", "output": [], "usage": {"input_tokens": 120, "output_tokens": 340}}).encode()


def recorder(root: Path) -> Recorder:
    return Recorder(root / "out", run_id="compile-gh1", provider="openai", model_id="gpt-6-luna", now=lambda: NOW)


class Writing(unittest.TestCase):
    def test_a_call_is_one_file_with_the_hash_of_the_request_bytes_and_the_parsed_bodies(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-A"
            rid = r.record_call(url="https://api.example.invalid/v1/responses", request=REQUEST, status=200, reply=REPLY, failure=None, projected_micros=500, cost_micros=190)
            self.assertEqual(rid, "rec-0001")
            doc = json.loads((Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text())
            self.assertEqual(doc["request_sha256"], "sha256:" + hashlib.sha256(REQUEST).hexdigest())
            self.assertEqual((doc["recording_id"], doc["run_id"], doc["case_id"], doc["provider"], doc["model_id"]), ("rec-0001", "compile-gh1", "G-A", "openai", "gpt-6-luna"))
            self.assertEqual(doc["recorded_at"], "2026-11-17T09:30:00Z")
            self.assertEqual((doc["status"], doc["failure"], doc["cost_micros"], doc["projected_micros"]), (200, None, 190, 500))
            self.assertEqual(doc["request"], json.loads(REQUEST))
            self.assertEqual(doc["reply"], json.loads(REPLY))
            self.assertEqual(doc["reported_model_id"], "gpt-6-luna-2026-10-01")

    def test_the_url_is_kept_as_a_path_only_and_no_host_or_header_is_written(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-A"
            r.record_call(url="https://api.example.invalid/v1/responses?x=1", request=REQUEST, status=200, reply=REPLY, failure=None, projected_micros=1, cost_micros=1)
            text = (Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text()
            self.assertIn("/v1/responses", text)
            for forbidden in ("api.example.invalid", "x=1", "Authorization", "Bearer"):
                self.assertNotIn(forbidden, text)

    def test_a_failed_call_is_recorded_with_its_failure_and_no_reply(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-B"
            r.record_call(url="https://h.invalid/v1/responses", request=REQUEST, status=None, reply=None, failure="TIMEOUT", projected_micros=500, cost_micros=500)
            doc = json.loads((Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text())
            self.assertEqual((doc["status"], doc["failure"], doc["reply"], doc["reported_model_id"]), (None, "TIMEOUT", None, None))

    # SR's condition on noema#654 (A320): the reply body of a provider response that is not 2xx is never recorded. A provider's error body can echo the request, name the account
    # or carry a key's tail; the status and the failure label say what happened, and nothing in the body is needed to replay it (UND-15).
    def test_the_reply_body_of_a_response_that_is_not_2xx_is_never_written(self) -> None:
        body = json.dumps({"error": {"message": "Incorrect API key provided: sk-live-ZZZZ9999. Account acct_SECRET is over its quota.", "model": "gpt-6-luna-2026-10-01"}}).encode()
        for status in (199, 300, 301, 400, 401, 403, 404, 408, 409, 422, 429, 500, 502, 503, 504):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as tmp:
                r = recorder(Path(tmp))
                r.case_id = "G-A"
                r.record_call(url="https://api.example.invalid/v1/responses", request=REQUEST, status=status, reply=body, failure=None, projected_micros=500, cost_micros=500)
                path = Path(tmp) / "out" / "recordings" / "rec-0001.json"
                text = path.read_text()
                doc = json.loads(text)
                self.assertEqual((doc["status"], doc["reply"], doc["reported_model_id"]), (status, None, None))
                for leaked in ("sk-live-ZZZZ9999", "acct_SECRET", "Incorrect API key", "over its quota"):
                    self.assertNotIn(leaked, text)
                manifest = json.loads(r.write_manifest({"pin": "x"}).read_text())
                self.assertNotIn("acct_SECRET", json.dumps(manifest))

    def test_a_2xx_reply_is_still_recorded_and_a_reply_with_no_status_is_not(self) -> None:
        for status in (200, 201, 202):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as tmp:
                r = recorder(Path(tmp))
                r.case_id = "G-A"
                r.record_call(url="https://api.example.invalid/v1/responses", request=REQUEST, status=status, reply=REPLY, failure=None, projected_micros=500, cost_micros=190)
                doc = json.loads((Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text())
                self.assertEqual((doc["reply"], doc["reported_model_id"]), (json.loads(REPLY), "gpt-6-luna-2026-10-01"))
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-A"
            r.record_call(url="https://api.example.invalid/v1/responses", request=REQUEST, status=None, reply=REPLY, failure=None, projected_micros=500, cost_micros=500)
            doc = json.loads((Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text())
            self.assertEqual((doc["reply"], doc["reported_model_id"]), (None, None))

    def test_a_body_that_is_not_json_is_kept_as_text_and_never_dropped(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-C"
            # A320 changed the premise: this test used a 502 "bad gateway" body and expected it kept, which is the behaviour SR asked to remove (a non-2xx body is never written, see the
            # test above). What it still guards is that a 2xx reply that is not JSON is kept as text and never dropped.
            r.record_call(url="https://h.invalid/p", request=b"not json", status=200, reply=b"<html>ok</html>", failure=None, projected_micros=1, cost_micros=1)
            doc = json.loads((Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text())
            self.assertEqual((doc["request"], doc["reply"]), ({"text": "not json"}, {"text": "<html>ok</html>"}))

    def test_ids_count_up_and_the_files_are_pretty_with_sorted_keys_so_a_line_is_one_value(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-A"
            ids = [r.record_call(url="https://h.invalid/p", request=REQUEST, status=200, reply=REPLY, failure=None, projected_micros=1, cost_micros=1) for _ in range(3)]
            self.assertEqual(ids, ["rec-0001", "rec-0002", "rec-0003"])
            text = (Path(tmp) / "out" / "recordings" / "rec-0001.json").read_text()
            self.assertGreater(len(text.splitlines()), 10)
            self.assertEqual(text, json.dumps(json.loads(text), indent=2, sort_keys=True) + "\n")

    def test_an_output_directory_that_already_holds_files_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "out").mkdir()
            (Path(tmp) / "out" / "old.json").write_text("{}")
            with self.assertRaises(ValueError):
                recorder(Path(tmp))


class Manifest(unittest.TestCase):
    def test_the_manifest_lists_every_recording_with_its_hash_and_carries_the_pin(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            r = recorder(Path(tmp))
            r.case_id = "G-A"
            r.record_call(url="https://h.invalid/p", request=REQUEST, status=200, reply=REPLY, failure=None, projected_micros=500, cost_micros=190)
            r.case_id = "G-B"
            r.record_call(url="https://h.invalid/p", request=b"{}", status=200, reply=REPLY, failure=None, projected_micros=500, cost_micros=200)
            path = r.write_manifest({"pinned_model_id": "gpt-6-luna", "rate_card": {"input_micro_per_mtok": 100000}})
            m = json.loads(path.read_text())
            self.assertEqual(path, Path(tmp) / "out" / "recordings" / "manifest.json")
            self.assertEqual((m["run_id"], m["provider"], m["model_id"], m["date"], m["calls"]), ("compile-gh1", "openai", "gpt-6-luna", "2026-11-17", 2))
            self.assertEqual(m["pin"], {"pinned_model_id": "gpt-6-luna", "rate_card": {"input_micro_per_mtok": 100000}})
            self.assertEqual([x["case_id"] for x in m["recordings"]], ["G-A", "G-B"])
            self.assertEqual(m["recordings"][0]["request_sha256"], "sha256:" + hashlib.sha256(REQUEST).hexdigest())
            self.assertEqual(m["reported_model_ids"], ["gpt-6-luna-2026-10-01"])
            self.assertEqual(m["label"], "Understanding: compile acceptance on synthetic golden examples, recorded live output (SYNTHETIC)")

    def test_a_manifest_of_no_calls_says_so(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            m = json.loads(recorder(Path(tmp)).write_manifest({}).read_text())
            self.assertEqual((m["calls"], m["recordings"], m["reported_model_ids"]), (0, [], []))


if __name__ == "__main__":
    unittest.main()
