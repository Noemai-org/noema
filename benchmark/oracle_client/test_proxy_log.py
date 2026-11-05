"""monetary_calls from the hostility proxy's per-route request log (Founder's decision of 4 Nov 2026, source (a); ENV-09, noema#159). The counter counts requests that were FORWARDED to the
system behind the proxy on a route that is not classified read-only, since the baseline. Written before the code and seen failing. SYNTHETIC. Generated layer only (INV-22)."""
import json
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any

from benchmark.oracle_client.counters import CounterUnavailable
from benchmark.oracle_client.proxy_log import READ_ONLY, ProxyRouteLog

Json = dict[str, Any]
ROUTES = json.loads((Path(__file__).resolve().parents[1] / "hostility_proxy" / "config" / "routes.json").read_text())["routes"]


def entry(seq: int, target: str = "mock-ledger", operation: str = "get_transaction", *, forwarded: bool = True, outcome: str = "answered", method: str = "GET") -> Json:
    return {"seq": seq, "target": target, "operation": operation, "method": method, "path": "/x", "call_no": 1, "fault": "", "forwarded": forwarded, "upstream_status": 200,
            "outcome": outcome, "status": 200, "request_body_sha256": "", "idempotency_key_sha256": ""}


class _Proxy(BaseHTTPRequestHandler):
    entries: list[Json] = []
    dropped = 0
    generation: str | None = None
    auth: list[str | None] = []
    status = 200

    def log_message(self, *a: Any) -> None:
        return

    def do_GET(self) -> None:
        _Proxy.auth.append(self.headers.get("Authorization"))
        document: Json = {"entries": _Proxy.entries, "counts": {}, "dropped": _Proxy.dropped, "log_hash": "sha256:" + "0" * 64}
        if _Proxy.generation is not None:
            document["generation"] = _Proxy.generation
        body = json.dumps(document).encode()
        self.send_response(_Proxy.status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class Counter(unittest.TestCase):
    def setUp(self) -> None:
        _Proxy.entries, _Proxy.dropped, _Proxy.auth, _Proxy.status = [], 0, [], 200
        self.server = HTTPServer(("127.0.0.1", 0), _Proxy)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.shutdown)
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        cred = Path(self._tmp.name) / "cred"
        cred.write_text("bench-credential\n")
        self.counter = ProxyRouteLog(f"http://127.0.0.1:{self.server.server_address[1]}", cred)

    def test_read_only_routes_are_not_monetary(self) -> None:
        mark = self.counter.mark()
        _Proxy.entries = [entry(1), entry(2, "mock-switch", "get_status"), entry(3, "mock-customer", "post_lookup", method="POST")]
        self.assertEqual(0, self.counter.since(mark))

    def test_a_forwarded_request_on_a_route_not_classified_read_only_is_a_monetary_call(self) -> None:
        mark = self.counter.mark()
        _Proxy.entries = [entry(1), entry(2, "mock-ledger", "create_payout", method="POST")]
        self.assertEqual(1, self.counter.since(mark))

    def test_a_request_the_proxy_refused_never_reached_a_system_and_is_not_counted(self) -> None:
        mark = self.counter.mark()
        _Proxy.entries = [entry(1, "-", "-", forwarded=False, outcome="refused", method="POST")]
        self.assertEqual(0, self.counter.since(mark))

    def test_only_requests_after_the_baseline_count(self) -> None:
        _Proxy.entries = [entry(1, "mock-ledger", "create_payout", method="POST")]
        mark = self.counter.mark()
        _Proxy.entries.append(entry(2, "mock-ledger", "reverse_payout", method="POST"))
        self.assertEqual(1, self.counter.since(mark))

    def test_a_write_whose_answer_was_dropped_still_counts_because_it_was_forwarded(self) -> None:
        mark = self.counter.mark()
        _Proxy.entries = [entry(1, "mock-ledger", "create_payout", outcome="dropped", method="POST")]
        self.assertEqual(1, self.counter.since(mark))

    def test_a_log_that_lost_entries_is_unavailable_not_zero(self) -> None:
        mark = self.counter.mark()
        _Proxy.dropped = 5
        with self.assertRaises(CounterUnavailable) as ctx:
            self.counter.since(mark)
        self.assertIn("dropped", ctx.exception.reason)

    def test_a_log_that_shrank_was_reset_during_the_case_and_is_unavailable(self) -> None:
        _Proxy.entries = [entry(1), entry(2)]
        mark = self.counter.mark()
        _Proxy.entries = [entry(1)]
        with self.assertRaises(CounterUnavailable):
            self.counter.since(mark)

    def test_a_reset_that_refills_past_the_marked_length_is_detected_not_undercounted(self) -> None:
        # Codex review of noema#170: remember only len(entries) and a restart that refills the in-memory log past it would slice at the old offset and miss calls.
        _Proxy.entries = [entry(1), entry(2)]
        mark = self.counter.mark()
        _Proxy.entries = [entry(1, "mock-ledger", "create_payout", method="POST"), entry(2, "mock-ledger", "reverse_payout", method="POST"), entry(3, "mock-ledger", "create_payout", method="POST")]
        with self.assertRaises(CounterUnavailable) as ctx:
            self.counter.since(mark)
        self.assertIn("changed", ctx.exception.reason)

    def test_a_log_whose_marked_entry_changed_is_detected_even_at_the_same_length(self) -> None:
        _Proxy.entries = [entry(1), entry(2)]
        mark = self.counter.mark()
        _Proxy.entries = [entry(1), entry(2, "mock-switch", "get_status")]
        with self.assertRaises(CounterUnavailable):
            self.counter.since(mark)

    def test_a_change_of_the_proxys_log_generation_is_detected_when_the_proxy_reports_one(self) -> None:
        _Proxy.entries, _Proxy.generation = [entry(1)], "g1"
        mark = self.counter.mark()
        _Proxy.generation = "g2"
        try:
            with self.assertRaises(CounterUnavailable) as ctx:
                self.counter.since(mark)
            self.assertIn("generation", ctx.exception.reason)
        finally:
            _Proxy.generation = None

    def test_an_unchanged_prefix_with_new_entries_counts_only_the_new_ones(self) -> None:
        _Proxy.entries = [entry(1), entry(2, "mock-ledger", "create_payout", method="POST")]
        mark = self.counter.mark()
        _Proxy.entries.append(entry(3, "mock-ledger", "reverse_payout", method="POST"))
        self.assertEqual(1, self.counter.since(mark))

    def test_an_unreachable_proxy_is_unavailable_not_zero(self) -> None:
        cred = Path(self._tmp.name) / "cred"
        with self.assertRaises(CounterUnavailable):
            ProxyRouteLog("http://127.0.0.1:9", cred, timeout=0.5).mark()

    def test_the_bench_credential_is_sent_and_a_refusal_is_unavailable(self) -> None:
        self.counter.mark()
        self.assertEqual("Bearer bench-credential", _Proxy.auth[0])
        _Proxy.status = 401
        with self.assertRaises(CounterUnavailable):
            self.counter.mark()

    def test_it_says_where_the_number_came_from(self) -> None:
        self.assertIn("request log", self.counter.describe())
        self.assertIn("forwarded", self.counter.describe())


class WithTheClient(unittest.TestCase):
    """The counter plugs into the oracle client: the baseline marks the log, the readout counts what came after."""

    def test_the_readout_counts_monetary_calls_since_the_baseline_and_the_manifest_names_the_source(self) -> None:
        from benchmark.oracle_client import testdata as td
        from benchmark.oracle_client.client import OracleClient
        from benchmark.oracle_client.counters import RecordGovernance, RefusalLog
        from benchmark.oracle_client.test_oracle import truth
        _Proxy.entries, _Proxy.dropped, _Proxy.auth, _Proxy.status = [entry(1, "mock-ledger", "create_payout", method="POST")], 0, [], 200
        server = HTTPServer(("127.0.0.1", 0), _Proxy)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        with td.StubBench() as stub, tempfile.TemporaryDirectory() as d:
            cred, log = Path(d) / "cred", Path(d) / "m.log"
            cred.write_text("bench-credential\n")
            log.write_text("")
            counter = ProxyRouteLog(f"http://127.0.0.1:{server.server_address[1]}", cred)
            client = OracleClient(truth(stub), refusal_log=RefusalLog(log), governance=RecordGovernance("MODEL"), monetary=counter)
            baseline = client.baseline()
            _Proxy.entries.append(entry(2, "mock-ledger", "reverse_payout", method="POST"))
            self.assertEqual(1, client.readout(baseline, [])["monetary_calls"])
            self.assertIn("request log", client.sources()["monetary_calls"])
            self.assertEqual([], [m for m in client.missing_sources() if m.startswith("monetary_calls")])


class Classification(unittest.TestCase):
    """The drift guard: a route added to the proxy's table is not silently read-only. Adding one forces someone to classify it here."""

    def test_every_route_in_the_proxy_table_is_classified_and_every_classified_route_exists(self) -> None:
        table = {(r["target"], r["operation"]) for r in ROUTES}
        self.assertEqual(table, set(READ_ONLY))

    def test_no_classified_operation_is_a_payout_or_a_reversal(self) -> None:
        for _, operation in READ_ONLY:
            for word in ("payout", "reverse", "refund", "transfer", "adjust", "create", "move"):
                self.assertNotIn(word, operation)


if __name__ == "__main__":
    unittest.main()
