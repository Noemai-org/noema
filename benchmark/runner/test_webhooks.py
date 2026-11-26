"""BEN-03, A667, A682: the runner's real WebhookSender. It calls mock-case's bench control routes (`POST /control/v1/webhooks/deliver` and `POST /control/v1/webhooks/{message_id}/replay`) and invents no event body:
the caller's file gives the object for each kind. Run against a real TCP server (AGENT-12). SYNTHETIC."""
import json
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any

from benchmark.runner.__main__ import main
from benchmark.runner.control import MockCaseControl
from benchmark.runner.test_suite import suite_dir
from benchmark.runner.webhooks import MockCaseWebhooks, WebhookNotDelivered, read_payloads

PAYLOADS = {"settlement_updated": {"synthetic_marker": "settlement"}, "reversal_posted": {"synthetic_marker": "reversal"}}


class Fake:
    """mock-case's bench routes, as documented in status/ENV-05.md: an answer for the relay's status, 404 when not configured, 422 when not delivered."""

    def __init__(self, status: int = 200, body: dict[str, Any] | None = None) -> None:
        self.seen: list[tuple[str, str, dict[str, Any], str]] = []
        outer = self
        reply_status, reply_body = status, body if body is not None else {"relay_status": 200, "accepted": True, "signed_at": "WALL_CLOCK"}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802
                raw = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                outer.seen.append((self.command, self.path, json.loads(raw or b"{}"), self.headers.get("Authorization", "")))
                data = json.dumps(reply_body).encode()
                self.send_response(reply_status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, *args: Any) -> None:
                return

        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.server.server_port}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()


class Sender(unittest.TestCase):
    def sender(self, fake: Fake, payloads: dict[str, Any] | None = None) -> MockCaseWebhooks:
        return MockCaseWebhooks(MockCaseControl(fake.url, "bench-secret"), PAYLOADS if payloads is None else payloads)

    def test_a_delivery_posts_the_given_object_with_the_bench_credential_a682(self) -> None:
        fake = Fake()
        self.addCleanup(fake.close)
        self.sender(fake).deliver("BEN-CLEAN-0001", "settlement_updated")
        method, path, body, auth = fake.seen[0]
        self.assertEqual((method, path), ("POST", "/control/v1/webhooks/deliver"))
        self.assertEqual(body, {"message_id": "BEN-CLEAN-0001:settlement_updated", "payload": PAYLOADS["settlement_updated"]})
        self.assertEqual(auth, "Bearer bench-secret")

    def test_the_message_id_is_the_same_for_the_same_case_and_event_and_differs_between_them(self) -> None:
        fake = Fake()
        self.addCleanup(fake.close)
        sender = self.sender(fake)
        sender.deliver("BEN-A-0001", "settlement_updated")
        sender.deliver("BEN-A-0001", "settlement_updated")
        sender.deliver("BEN-A-0001", "reversal_posted")
        sender.deliver("BEN-B-0002", "settlement_updated")
        ids = [seen[2]["message_id"] for seen in fake.seen]
        self.assertEqual(ids[0], ids[1])
        self.assertEqual(len(set(ids)), 3)

    def test_a_kind_with_no_object_is_not_delivered_and_nothing_is_sent(self) -> None:
        fake = Fake()
        self.addCleanup(fake.close)
        with self.assertRaises(WebhookNotDelivered):
            self.sender(fake, {"settlement_updated": {"a": 1}}).deliver("BEN-A-0001", "reversal_posted")
        self.assertEqual(fake.seen, [])

    def test_a_relay_that_did_not_accept_is_a_failed_delivery_not_an_ok_one(self) -> None:
        fake = Fake(200, {"relay_status": 401, "accepted": False})
        self.addCleanup(fake.close)
        with self.assertRaises(WebhookNotDelivered):
            self.sender(fake).deliver("BEN-A-0001", "settlement_updated")

    def test_mock_case_without_webhook_settings_is_a_failed_delivery(self) -> None:
        fake = Fake(404, {"error": {"code": "WEBHOOK_NOT_CONFIGURED", "message": "none", "retryable": False}})
        self.addCleanup(fake.close)
        with self.assertRaises(Exception):
            self.sender(fake).deliver("BEN-A-0001", "settlement_updated")

    def test_a_replay_posts_to_the_message_id_route_and_needs_acceptance(self) -> None:
        fake = Fake()
        self.addCleanup(fake.close)
        self.sender(fake).replay("BEN-A-0001", "settlement_updated")
        self.assertEqual(fake.seen[0][:2], ("POST", "/control/v1/webhooks/BEN-A-0001:settlement_updated/replay"))


class Payloads(unittest.TestCase):
    def write(self, value: Any) -> Path:
        d = tempfile.mkdtemp()
        self.addCleanup(lambda: [p.unlink() for p in Path(d).iterdir()] and Path(d).rmdir())
        path = Path(d) / "payloads.json"
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def test_a_file_of_the_two_kinds_is_read(self) -> None:
        self.assertEqual(read_payloads(self.write(PAYLOADS)), PAYLOADS)

    def test_a_kind_the_runner_does_not_drive_an_empty_object_or_a_non_object_is_refused(self) -> None:
        for bad in ({"forged_signature": {"a": 1}}, {"settlement_updated": {}}, {"settlement_updated": [1]}, ["settlement_updated"], {}):
            with self.assertRaises(ValueError, msg=str(bad)):
                read_payloads(self.write(bad))


class Cli(unittest.TestCase):
    def test_the_flag_makes_the_webhook_cases_runnable_in_the_report_and_without_it_they_are_not(self) -> None:
        import io
        from contextlib import redirect_stdout

        def report(*extra: str) -> str:
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(main(["--suite", str(suite_dir()), "--out", "/tmp/unused-o", "--support", *extra]), 0)
            return out.getvalue()

        without = report()
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / "p.json"
            path.write_text(json.dumps(PAYLOADS), encoding="utf-8")
            with_flag = report("--webhook-payloads", str(path))
        self.assertIn("event: webhook", without)
        self.assertNotIn("event: webhook", with_flag)


if __name__ == "__main__":
    unittest.main()
