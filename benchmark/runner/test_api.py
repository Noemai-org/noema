"""The public API client: only the endpoints of PERD §39.2 (and the §39.4 reads and kill switch), as a partner app would call them (REQ-BEN-002 step 3)."""
import unittest

from benchmark.oracle_client.transport import HttpError
from benchmark.runner.api import IN_FLIGHT, NotSettled, PublicApi
from benchmark.runner.testdata import ADMIN_TOKEN, CORRELATION, USER_TOKEN, FakeNoema, Script


class Api(unittest.TestCase):
    def test_start_a_conversation(self) -> None:
        with FakeNoema() as fake:
            conv = PublicApi(fake.url).start(USER_TOKEN, "req_0001", "Where is my money?", locale_hint="pcm")
            self.assertEqual((conv.investigation_id, conv.conversation_ref, conv.state, conv.correlation_id), ("inv_0001", "conv_0001", "RECEIVED", CORRELATION))
            method, path, auth, body = fake.requests[0]
            self.assertEqual((method, path, auth), ("POST", "/v1/conversations", f"Bearer {USER_TOKEN}"))
            self.assertEqual(body, {"request_id": "req_0001", "text": "Where is my money?", "locale_hint": "pcm"})

    def test_locale_hint_is_omitted_when_not_given(self) -> None:
        with FakeNoema() as fake:
            PublicApi(fake.url).start(USER_TOKEN, "req_0001", "hi")
            self.assertNotIn("locale_hint", fake.requests[0][3] or {})

    def test_a_follow_up_and_a_confirmation(self) -> None:
        with FakeNoema() as fake:
            api = PublicApi(fake.url)
            self.assertEqual(api.message(USER_TOKEN, "conv_0001", "req_0002", text="The one ending 1234"), "RECEIVED")
            self.assertEqual(api.confirm(USER_TOKEN, "cnf_0001", "req_0003", "ACCEPT"), "EVALUATING")
            self.assertEqual(fake.requests[0][3], {"request_id": "req_0002", "text": "The one ending 1234"})
            self.assertEqual(fake.requests[1][1:2], ("/v1/confirmations/cnf_0001",))
            self.assertEqual(fake.requests[1][3], {"request_id": "req_0003", "response": "ACCEPT"})

    def test_wait_settled_polls_until_the_state_leaves_the_in_flight_set(self) -> None:
        with FakeNoema(Script(states=["INTERPRETING", "EVALUATING", "AWAITING_CLARIFICATION"])) as fake:
            sleeps: list[float] = []
            state = PublicApi(fake.url).wait_settled(USER_TOKEN, "inv_0001", timeout_s=5, poll_s=0.01, sleep=sleeps.append)
            self.assertEqual(state, "AWAITING_CLARIFICATION")
            self.assertEqual(len(sleeps), 2)

    def test_wait_settled_gives_up(self) -> None:
        with FakeNoema(Script(states=["EVALUATING"])) as fake:
            ticks = iter(range(100))
            with self.assertRaises(NotSettled):
                PublicApi(fake.url).wait_settled(USER_TOKEN, "inv_0001", timeout_s=3, poll_s=1, sleep=lambda s: None, now=lambda: float(next(ticks)))

    def test_the_in_flight_states_are_the_perd_state_machines(self) -> None:
        for state in ("RECEIVED", "INTERPRETING", "EVALUATING", "PLANNED", "EXECUTING", "RECOVERING", "ESCALATING", "EXPLAINING"):
            self.assertIn(state, IN_FLIGHT)
        for state in ("COMPLETED", "ESCALATED", "REJECTED", "EXPIRED", "AWAITING_CLARIFICATION", "AWAITING_CONFIRMATION", "BLOCKED_UNKNOWN"):
            self.assertNotIn(state, IN_FLIGHT)

    def test_server_sent_events_are_parsed(self) -> None:
        script = Script(sse=[("progress", {"state": "EVALUATING"}), ("confirmation", {"confirmation_request_id": "cnf_0009"})])
        with FakeNoema(script) as fake:
            events = PublicApi(fake.url).events(USER_TOKEN, "conv_0001", timeout_s=2)
            self.assertEqual([name for name, _ in events], ["progress", "confirmation"])
            self.assertEqual(PublicApi.confirmation_id(events), "cnf_0009")

    def test_no_confirmation_event_gives_none(self) -> None:
        self.assertIsNone(PublicApi.confirmation_id([("progress", {})]))

    def test_the_reconstruction_is_fetched_by_correlation_id(self) -> None:
        with FakeNoema() as fake:
            body = PublicApi(fake.url).reconstruction(USER_TOKEN, CORRELATION)
            self.assertEqual(body["correlation_id"], CORRELATION)
            self.assertEqual(fake.requests[0][1], f"/v1/reconstructions/{CORRELATION}")

    def test_a_refused_session_is_an_error_with_the_code(self) -> None:
        with FakeNoema() as fake:
            with self.assertRaises(HttpError) as raised:
                PublicApi(fake.url).start("wrong", "req_0001", "hi")
            self.assertEqual(raised.exception.code, "SESSION_INVALID")

    def test_the_kill_switch_uses_the_admin_credential(self) -> None:
        with FakeNoema() as fake:
            PublicApi(fake.url).kill_switch(ADMIN_TOKEN, "ten_synth", True, "benchmark case")
            self.assertEqual(fake.requests[0][1], "/v1/admin/tenants/ten_synth/kill-switch")
            self.assertEqual(fake.requests[0][3], {"enabled": True, "reason": "benchmark case"})

    def test_every_call_stays_on_the_public_api(self) -> None:
        with FakeNoema() as fake:
            api = PublicApi(fake.url)
            conv = api.start(USER_TOKEN, "req_0001", "hi")
            api.wait_settled(USER_TOKEN, conv.investigation_id, timeout_s=5, poll_s=0.0, sleep=lambda s: None)
            api.reconstruction(USER_TOKEN, conv.correlation_id)
            self.assertTrue(all(p.startswith("/v1/") for p in fake.paths()))
            self.assertFalse([p for p in fake.paths() if "truth" in p or "oracle" in p or "internal" in p or "benchmark" in p])


if __name__ == "__main__":
    unittest.main()
