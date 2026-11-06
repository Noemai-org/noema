"""Option B for G1 (Founder, 4 Nov 2026): a Noema-side stand-in for the US-01 and US-06 data, evidence labelled "stand-in". This is the stand-in's DATA CONTRACT: the derivation of the
PERD 34.4 normalised payloads (`fin.ledger_entry`, `fin.switch_record`, `fin.destination_status`, the principal context and the candidate list) from a `benchmark-fixture`, as a pure function,
and the loader that hands the result to the stand-in's control route. Written before the code and seen failing. SYNTHETIC (AGENT-25). Generated layer only (INV-22)."""
import json
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any

from benchmark.fixtures.loader import StandInFixtureLoader
from benchmark.fixtures.standin import CLOCK_START, LABEL, derive
from benchmark.runner.env import EnvironmentUnavailable
from benchmark.runner.test_suite import suite_dir

Json = dict[str, Any]


def fixture(**tx: Any) -> Json:
    base = {"ref": "fin.transaction:TX1", "owner": "usr_a", "beneficiary": "ben_0001", "direction": "OUTGOING", "amount_minor": 250000, "currency": "NGN", "age_minutes": 150,
            "ledger": "POSTED", "switch": "APPROVED", "destination": "CREDITED", "settlement": "SETTLED"}
    base.update(tx)
    return {"fixture_id": "fx_test", "synthetic": True, "principals": [{"principal_id": "usr_a", "display_name": "A", "roles": ["customer"]},
                                                                      {"principal_id": "usr_b", "display_name": "B", "roles": ["customer"]}],
            "beneficiaries": [{"beneficiary_id": "ben_0001", "owner": "usr_a", "name": "Chidi Okonkwo", "account_last4": "3644", "aliases": ["Chidi"]}],
            "transactions": [base], "cases": [], "enterprise_strings": {}}


class Derive(unittest.TestCase):
    def test_the_label_is_stand_in_and_every_payload_says_synthetic(self) -> None:
        out = derive(fixture())
        self.assertEqual("stand-in", LABEL)
        self.assertEqual((LABEL, True), (out["data_source"], out["synthetic"]))

    def test_a_posted_outgoing_transaction_has_one_posted_debit(self) -> None:
        entries = derive(fixture())["ledger_entries"]["fin.transaction:TX1"]["entries"]
        self.assertEqual([("DEBIT", "POSTED", 250000, "NGN")], [(e["entry_type"], e["posting_status"], e["amount_minor"], e["currency"]) for e in entries])

    def test_an_incoming_transaction_is_a_credit(self) -> None:
        entries = derive(fixture(direction="INCOMING"))["ledger_entries"]["fin.transaction:TX1"]["entries"]
        self.assertEqual("CREDIT", entries[0]["entry_type"])

    def test_not_posted_is_an_answer_with_no_entry_and_unavailable_is_an_unavailable_answer(self) -> None:
        none = derive(fixture(ledger="NOT_POSTED"))["ledger_entries"]["fin.transaction:TX1"]
        self.assertEqual(([], False), (none["entries"], none["unavailable"]))
        down = derive(fixture(ledger="UNAVAILABLE"))["ledger_entries"]["fin.transaction:TX1"]
        self.assertEqual(([], True), (down["entries"], down["unavailable"]))

    def test_a_reversed_ledger_has_the_original_marked_reversed_and_a_reversal(self) -> None:
        entries = derive(fixture(ledger="REVERSED"))["ledger_entries"]["fin.transaction:TX1"]["entries"]
        self.assertEqual([("DEBIT", "REVERSED"), ("REVERSAL", "POSTED")], [(e["entry_type"], e["posting_status"]) for e in entries])

    def test_switch_statuses_keep_the_raw_code_and_the_semantic_status_side_by_side(self) -> None:
        expected = {"APPROVED": ("ACCEPTED", True), "DECLINED": ("REJECTED", True), "TIMED_OUT": ("TIMED_OUT", False), "PENDING": ("PENDING", False)}
        for raw, (semantic, terminal) in expected.items():
            record = derive(fixture(switch=raw))["switch_records"]["fin.transaction:TX1"]
            self.assertEqual((semantic, terminal, False), (record["semantic_status"], record["is_terminal"], record["unavailable"]), raw)
            self.assertTrue(record["raw_code"].startswith("SYN-"), raw)
        self.assertTrue(derive(fixture(switch="UNAVAILABLE"))["switch_records"]["fin.transaction:TX1"]["unavailable"])

    def test_destination_and_settlement_are_reported_with_effective_and_observation_times(self) -> None:
        d = derive(fixture(destination="PENDING", settlement="REVERSAL_PENDING"))["destination_statuses"]["fin.transaction:TX1"]
        self.assertEqual(("PENDING", "REVERSAL_PENDING"), (d["credit_state"], d["settlement_state"]))
        self.assertLess(d["effective_at"], d["observed_at"])
        self.assertEqual(CLOCK_START, d["observed_at"])

    def test_the_transaction_time_is_the_clock_start_minus_age(self) -> None:
        c = derive(fixture(age_minutes=150))["candidates"]["usr_a"][0]
        self.assertEqual("2026-10-01T07:10:00+01:00", c["occurred_at"])

    def test_candidates_and_beneficiaries_are_scoped_to_their_owner_never_global(self) -> None:
        out = derive(fixture())
        self.assertEqual(["fin.transaction:TX1"], [c["transaction_ref"] for c in out["candidates"]["usr_a"]])
        self.assertEqual([], out["candidates"]["usr_b"])
        self.assertEqual(["ben_0001"], [b["beneficiary_id"] for b in out["principals"]["usr_a"]["beneficiaries"]])
        self.assertEqual([], out["principals"]["usr_b"]["beneficiaries"])

    def test_candidate_lists_carry_an_explicit_completeness_marker(self) -> None:
        self.assertTrue(derive(fixture())["candidate_lists_complete"])

    def test_an_unknown_status_is_refused_not_guessed(self) -> None:
        with self.assertRaises(ValueError) as ctx:
            derive(fixture(switch="MAYBE"))
        self.assertIn("MAYBE", str(ctx.exception))

    def test_the_same_fixture_gives_byte_identical_output(self) -> None:
        self.assertEqual(json.dumps(derive(fixture()), sort_keys=True), json.dumps(derive(fixture()), sort_keys=True))

    def test_every_fixture_of_the_generated_suite_derives(self) -> None:
        count = 0
        for path in sorted((suite_dir() / "fixtures").rglob("*.json")):
            out = derive(json.loads(path.read_text()))
            self.assertTrue(out["synthetic"], path.name)
            count += 1
        self.assertGreaterEqual(count, 300)

    def test_a_fixture_with_existing_cases_is_marked_as_needing_a_case_seed(self) -> None:
        fx = fixture()
        fx["cases"] = [{"case_id": "c1"}]
        self.assertEqual(1, derive(fx)["existing_cases"])


class _Stand(BaseHTTPRequestHandler):
    seen: list[tuple[str, str, str | None, bytes]] = []
    status = 204

    def log_message(self, *a: Any) -> None:
        return

    def do_PUT(self) -> None:
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        _Stand.seen.append((self.command, self.path, self.headers.get("Authorization"), body))
        self.send_response(_Stand.status)
        self.end_headers()


class Loader(unittest.TestCase):
    def setUp(self) -> None:
        _Stand.seen, _Stand.status = [], 204
        self.server = HTTPServer(("127.0.0.1", 0), _Stand)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.shutdown)
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        cred = Path(self._tmp.name) / "cred"
        cred.write_text("bench-credential\n")
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.loader = StandInFixtureLoader(self.url, cred)

    def test_it_puts_the_derived_payloads_with_the_bench_credential(self) -> None:
        self.loader.load(fixture())
        method, path, auth, body = _Stand.seen[0]
        self.assertEqual(("PUT", "/control/v1/standin", "Bearer bench-credential"), (method, path, auth))
        self.assertEqual(derive(fixture()), json.loads(body))

    def test_a_refusal_by_the_stand_in_is_an_unavailable_loader_not_a_silent_pass(self) -> None:
        _Stand.status = 401
        with self.assertRaises(EnvironmentUnavailable) as ctx:
            self.loader.load(fixture())
        self.assertEqual("fixture loader", ctx.exception.component)

    def test_an_unreachable_stand_in_is_an_unavailable_loader(self) -> None:
        cred = Path(self._tmp.name) / "cred"
        with self.assertRaises(EnvironmentUnavailable):
            StandInFixtureLoader("http://127.0.0.1:9", cred, timeout=0.5).load(fixture())

    def test_existing_cases_are_refused_until_mock_case_has_a_seed_route(self) -> None:
        fx = fixture()
        fx["cases"] = [{"case_id": "c1"}]
        with self.assertRaises(EnvironmentUnavailable) as ctx:
            self.loader.load(fx)
        self.assertIn("seed", ctx.exception.reason)
        self.assertEqual([], _Stand.seen)

    def test_the_loader_has_no_unavailable_reason_when_it_is_configured(self) -> None:
        self.assertIsNone(getattr(self.loader, "unavailable_reason", None))


if __name__ == "__main__":
    unittest.main()
