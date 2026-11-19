"""BEN-05 / A444 (rulings of 20 Nov): the runner's own per-request record, `requests.jsonl`, for the G1 stack's RECORD_ACCESSED count (noema#730: the expected count is derived from this record). Written
before `request_log.py` and seen failing. SYNTHETIC.

One JSON object per line with exactly `case_id`, `correlation_id`, `method`, `path`, `status` (an integer) and `kind` ("case", "reconstruction" or "probe"). No header, no body and no token is ever
recorded, and the path is the route TEMPLATE (`/v1/reconstructions/{correlation_id}`), never the concrete identifiers (a leak scan flags digit runs; a template carries none). Only the reads of the
reconstruction route (the audit read that writes a RECORD_ACCESSED line) are kind "reconstruction"."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.oracle_client.transport import HttpError, JsonClient
from benchmark.runner.api import PublicApi
from benchmark.runner.request_log import FIELDS, RequestRecorder
from benchmark.runner.testdata import CORRELATION, USER_TOKEN, FakeNoema, Script

Json = dict[str, Any]


def lines(recorder: RequestRecorder) -> list[Json]:
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "requests.jsonl"
        recorder.write(path)
        return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()]


class Recorder(unittest.TestCase):
    def test_a_case_s_requests_get_its_case_id_and_its_correlation_when_the_case_ends(self) -> None:
        r = RequestRecorder()
        r.begin("BEN-A-0001")
        r.record("POST", "/v1/conversations", 201)
        r.record("GET", "/v1/reconstructions/{correlation_id}", 200)
        r.end("BEN-A-0001", "corr_a")
        got = lines(r)
        self.assertEqual([{"case_id": "BEN-A-0001", "correlation_id": "corr_a", "method": "POST", "path": "/v1/conversations", "status": 201, "kind": "case"},
                          {"case_id": "BEN-A-0001", "correlation_id": "corr_a", "method": "GET", "path": "/v1/reconstructions/{correlation_id}", "status": 200, "kind": "reconstruction"}], got)
        for line in got:
            self.assertEqual(set(FIELDS), set(line))
            self.assertIs(int, type(line["status"]))

    def test_only_the_reconstruction_route_is_kind_reconstruction(self) -> None:
        r = RequestRecorder()
        r.begin("BEN-A-0001")
        for method, path in (("POST", "/v1/conversations"), ("POST", "/v1/conversations/{conversation_ref}/messages"), ("POST", "/v1/confirmations/{confirmation_request_id}"),
                             ("GET", "/v1/investigations/{investigation_id}"), ("POST", "/v1/admin/tenants/{tenant_id}/kill-switch"), ("GET", "/v1/reconstructions/{correlation_id}")):
            r.record(method, path, 200)
        r.end("BEN-A-0001", "corr_a")
        self.assertEqual(["case"] * 5 + ["reconstruction"], [x["kind"] for x in lines(r)])

    def test_a_case_with_no_correlation_records_nothing_never_an_empty_correlation(self) -> None:
        r = RequestRecorder()
        r.begin("BEN-A-0001")
        r.record("POST", "/v1/conversations", 500)
        r.end("BEN-A-0001", None)
        r.end("BEN-A-0002", "")  # a case that was never begun
        self.assertEqual([], lines(r))

    def test_ending_another_case_neither_records_nor_closes_the_one_in_progress(self) -> None:
        r = RequestRecorder()
        r.begin("BEN-A-0001")
        r.record("POST", "/v1/conversations", 202)
        r.end("BEN-OTHER-0001", "corr_x")  # not the case in progress
        self.assertEqual([], lines(r))
        r.record("GET", "/v1/investigations/{investigation_id}", 200)
        r.end("BEN-A-0001", "corr_a")
        self.assertEqual(["BEN-A-0001", "BEN-A-0001"], [x["case_id"] for x in lines(r)])

    def test_requests_outside_a_case_are_not_recorded(self) -> None:
        r = RequestRecorder()
        r.record("POST", "/v1/admin/tenants/{tenant_id}/kill-switch", 200)
        r.begin("BEN-A-0001")
        r.record("GET", "/v1/investigations/{investigation_id}", 200)
        r.end("BEN-A-0001", "corr_a")
        r.record("GET", "/v1/investigations/{investigation_id}", 200)
        self.assertEqual(1, len(lines(r)))

    def test_cases_are_kept_in_the_order_they_ran_and_each_request_in_the_order_it_was_sent(self) -> None:
        r = RequestRecorder()
        for case, corr in (("BEN-B-0001", "corr_b"), ("BEN-A-0001", "corr_a")):
            r.begin(case)
            r.record("POST", "/v1/conversations", 201)
            r.record("GET", "/v1/investigations/{investigation_id}", 200)
            r.end(case, corr)
        self.assertEqual(["BEN-B-0001", "BEN-B-0001", "BEN-A-0001", "BEN-A-0001"], [x["case_id"] for x in lines(r)])

    def test_a_status_that_is_not_an_integer_or_a_method_that_is_not_text_is_refused(self) -> None:
        r = RequestRecorder()
        r.begin("BEN-A-0001")
        for status in (True, "200", 200.0, None):
            with self.assertRaises(ValueError, msg=repr(status)):
                r.record("GET", "/v1/investigations/{investigation_id}", status)  # type: ignore[arg-type]
        for method in ("", None, 3):
            with self.assertRaises(ValueError, msg=repr(method)):
                r.record(method, "/x", 200)  # type: ignore[arg-type]

    def test_a_concrete_path_is_refused_so_no_identifier_can_reach_the_file(self) -> None:
        r = RequestRecorder()
        r.begin("BEN-A-0001")
        for path in ("/v1/reconstructions/corr_01HABCDEF12345", "/v1/conversations/conv_0001/messages", "/v1/investigations/inv_0001", "v1/x", "/v1/x?after=1"):
            with self.assertRaises(ValueError, msg=path):
                r.record("GET", path, 200)

    def test_the_file_is_empty_for_a_run_with_no_request_never_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "requests.jsonl"
            RequestRecorder().write(path)
            self.assertTrue(path.is_file())
            self.assertEqual("", path.read_text(encoding="utf-8"))


class Transport(unittest.TestCase):
    def test_the_client_exposes_the_status_of_the_last_answer(self) -> None:
        with FakeNoema() as fake:
            client = JsonClient(fake.url, token=USER_TOKEN)
            self.assertIsNone(client.last_status)
            client.request("GET", "/v1/investigations/inv_0001")
            self.assertEqual(200, client.last_status)


class Api(unittest.TestCase):
    def test_every_call_is_recorded_as_a_route_template_with_its_status(self) -> None:
        with FakeNoema() as fake:
            r = RequestRecorder()
            api = PublicApi(fake.url, recorder=r)
            r.begin("BEN-A-0001")
            conv = api.start(USER_TOKEN, "req_0001", "hi")
            api.message(USER_TOKEN, "conv_0001", "req_0002", text="x")
            api.confirm(USER_TOKEN, "cnf_0001", "req_0003", "ACCEPT")
            api.investigation(USER_TOKEN, "inv_0001")
            api.reconstruction(USER_TOKEN, CORRELATION)
            r.end("BEN-A-0001", conv.correlation_id)
        got = lines(r)
        self.assertEqual([("POST", "/v1/conversations", "case"), ("POST", "/v1/conversations/{conversation_ref}/messages", "case"), ("POST", "/v1/confirmations/{confirmation_request_id}", "case"),
                          ("GET", "/v1/investigations/{investigation_id}", "case"), ("GET", "/v1/reconstructions/{correlation_id}", "reconstruction")], [(x["method"], x["path"], x["kind"]) for x in got])
        self.assertTrue(all(x["status"] in (200, 202) and x["correlation_id"] == CORRELATION for x in got))
        text = json.dumps(got)
        for secret in (USER_TOKEN, "req_0001", "inv_0001", "cnf_0001", "Bearer"):
            self.assertNotIn(secret, text)

    def test_a_refused_request_records_its_status_and_still_raises(self) -> None:
        with FakeNoema(Script(start_status=429)) as fake:
            r = RequestRecorder()
            api = PublicApi(fake.url, recorder=r)
            r.begin("BEN-A-0001")
            with self.assertRaises(HttpError):
                api.start(USER_TOKEN, "req_0001", "hi")
            r.end("BEN-A-0001", "corr_a")
        got = lines(r)
        self.assertEqual(1, len(got), "a refusal is recorded once, not again as an answer")
        last = got[-1]
        self.assertEqual((429, "POST", "/v1/conversations"), (last["status"], last["method"], last["path"]))

    def test_an_api_with_no_recorder_behaves_as_before(self) -> None:
        with FakeNoema() as fake:
            api = PublicApi(fake.url)
            self.assertIsNone(api.recorder)
            self.assertEqual("inv_0001", api.start(USER_TOKEN, "req_0001", "hi").investigation_id)


if __name__ == "__main__":
    unittest.main()
