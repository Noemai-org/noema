"""A667: the runner drives `webhook:settlement_updated`, `webhook:reversal_posted` and `session_resume:same_channel`; any other event kind fails the case and is never skipped (REQ-BEN-002; AGENT-12).

What these tests prove:
- a webhook kind is driven only when the environment has a webhook sender, and only the two kinds the ruling names; with no sender the case is an ERROR (EVENT_KIND_UNSUPPORTED) and Noema is never called;
- a driven webhook is delivered exactly once, in timeline order with the other events, after the first input has settled and before the next turn;
- a sender that fails makes the case an ERROR (WEBHOOK_DELIVERY_FAILED) naming the class of the failure, never a skipped event and never a crash;
- a resume re-asks the session provider for the same principal and sends the later turns with the token it returns, in the same conversation, on the same channel; any other resume argument is unsupported;
- an event of a driven kind that sits at the start of the timeline, where nothing is driven, is an error and is never dropped."""
import unittest
from typing import Any

from benchmark.runner.drivers import driven
from benchmark.runner.run import run_case
from benchmark.runner.env import RunEnv
from benchmark.runner.test_run import Rig, Sessions, errors, plain_case, with_timeline
from benchmark.runner.testdata import ADMIN_TOKEN, USER_TOKEN, Script

Json = dict[str, Any]


class Sender:
    """A webhook sender that records what it was asked to deliver and, optionally, fails."""

    def __init__(self, fail: Exception | None = None, log: list[str] | None = None) -> None:
        self.delivered: list[tuple[str, str]] = []
        self.fail, self.log = fail, log if log is not None else []

    def deliver(self, case_id: str, event: str) -> None:
        self.log.append(f"webhook:{event}")
        if self.fail is not None:
            raise self.fail
        self.delivered.append((case_id, event))


def with_sender(env: RunEnv, sender: Sender | None) -> RunEnv:
    return RunEnv(**{**env.__dict__, "webhooks": sender})


class Driven(unittest.TestCase):
    def test_only_the_two_named_webhook_kinds_are_driven_and_only_with_a_sender(self) -> None:
        for name in ("settlement_updated", "reversal_posted"):
            self.assertTrue(driven(f"webhook:{name}", webhooks=True), name)
            self.assertFalse(driven(f"webhook:{name}"), name)
            self.assertFalse(driven(f"webhook:{name}", webhooks=False), name)
        for other in ("webhook:destination_credited", "webhook:forged_signature", "webhook:replayed", "webhook:", "webhook:settlement_updated:x", "webhook:Settlement_Updated"):
            self.assertFalse(driven(other, webhooks=True), other)

    def test_a_resume_on_the_same_channel_is_driven_and_no_other_resume_is(self) -> None:
        self.assertTrue(driven("session_resume:same_channel"))
        for other in ("session_resume:other_channel", "session_resume:", "session_resume", "session_resume:same_channel:x"):
            self.assertFalse(driven(other), other)

    def test_the_kinds_that_were_driven_before_still_are(self) -> None:
        for event in ("session_start:synthetic", "clock_advance:PT31S", "confirm:ACCEPT", "kill_switch:on"):
            self.assertTrue(driven(event), event)


class Webhooks(unittest.TestCase):
    def test_a_webhook_with_no_sender_is_an_error_and_noema_is_never_called(self) -> None:
        for name in ("settlement_updated", "reversal_posted"):
            with Rig() as rig:
                result = run_case(rig.env, with_timeline(plain_case(), [f"webhook:{name}"]))
                self.assertEqual(rig.noema.requests, [])
            self.assertEqual(result["error"]["code"], "EVENT_KIND_UNSUPPORTED", name)
            self.assertIn("webhook", result["error"]["message"])

    def test_a_driven_webhook_is_delivered_once_after_the_first_input_and_the_case_completes(self) -> None:
        for name in ("settlement_updated", "reversal_posted"):
            sender = Sender()
            lc = with_timeline(plain_case(), [f"webhook:{name}"])
            with Rig() as rig:
                result = run_case(with_sender(rig.env, sender), lc)
                started_before_delivery = any(path == "/v1/conversations" for _, path, _, _ in rig.noema.requests)
            self.assertEqual(result["status"], "COMPLETED", result["error"])
            self.assertEqual(errors(result), [])
            self.assertEqual(sender.delivered, [(lc.case["case_id"], name)], name)
            self.assertTrue(started_before_delivery)

    def test_events_are_driven_in_timeline_order_and_each_once(self) -> None:
        log: list[str] = []
        sender = Sender(log=log)
        lc = with_timeline(plain_case(), ["webhook:settlement_updated", "clock_advance:PT31S", "webhook:reversal_posted"])
        with Rig() as rig:
            real_advance = rig.env.control.advance

            def advance(by: str) -> Json:
                log.append(f"clock_advance:{by}")
                return real_advance(by)

            rig.env.control.advance = advance  # type: ignore[method-assign]
            result = run_case(with_sender(rig.env, sender), lc)
        self.assertEqual(result["status"], "COMPLETED", result["error"])
        self.assertEqual(log, ["webhook:settlement_updated", "clock_advance:PT31S", "webhook:reversal_posted"])

    def test_a_failing_sender_is_an_error_naming_the_class_and_never_a_skip(self) -> None:
        sender = Sender(fail=RuntimeError("secret detail that must not travel"))
        with Rig() as rig:
            result = run_case(with_sender(rig.env, sender), with_timeline(plain_case(), ["webhook:settlement_updated"]))
        self.assertEqual((result["status"], result["error"]["code"]), ("ERROR", "WEBHOOK_DELIVERY_FAILED"))
        self.assertIn("RuntimeError", result["error"]["message"])
        self.assertNotIn("secret detail", result["error"]["message"])
        self.assertEqual(errors(result), [])

    def test_one_unsupported_event_among_driven_ones_still_fails_the_whole_case(self) -> None:
        sender = Sender()
        lc = with_timeline(plain_case(), ["webhook:settlement_updated", "webhook:destination_credited"])
        with Rig() as rig:
            result = run_case(with_sender(rig.env, sender), lc)
            self.assertEqual(rig.noema.requests, [])
        self.assertEqual(result["error"]["code"], "EVENT_KIND_UNSUPPORTED")
        self.assertEqual(sender.delivered, [], "nothing is delivered for a case that cannot run in full")

    def test_a_driven_event_at_the_start_of_the_timeline_is_an_error_not_a_drop(self) -> None:
        sender = Sender()
        lc = with_timeline(plain_case(), ["webhook:reversal_posted"], at="+00:00:00")
        with Rig() as rig:
            result = run_case(with_sender(rig.env, sender), lc)
            self.assertEqual(rig.noema.requests, [])
            self.assertEqual(rig.loader.seen, [], "refused before any side effect: no fixture is loaded")
        self.assertEqual((result["status"], result["error"]["code"]), ("ERROR", "EVENT_AT_START_UNSUPPORTED"))
        self.assertEqual(sender.delivered, [])


class Resume(unittest.TestCase):
    def test_a_resume_asks_for_the_same_principal_again_and_sends_the_later_turn_with_that_token(self) -> None:
        lc = with_timeline(plain_case(), ["session_resume:same_channel"], turns=["The one ending 1234"])
        with Rig() as rig:
            tokens = iter([USER_TOKEN, ADMIN_TOKEN, USER_TOKEN, USER_TOKEN])

            def token_for(principal_id: str, roles: list[str]) -> str:
                rig.sessions.asked.append((principal_id, tuple(roles)))
                return next(tokens)

            rig.sessions.token_for = token_for  # type: ignore[method-assign]
            result = run_case(rig.env, lc)
            principal = (lc.case["principal"]["principal_id"], tuple(lc.case["principal"]["roles"]))
            asked = [a for a in rig.sessions.asked if a == principal]
            message = next((auth for method, path, auth, _ in rig.noema.requests if method == "POST" and path.endswith("/messages")), None)
        self.assertEqual(result["status"], "COMPLETED", result["error"])
        self.assertEqual(len(asked), 2, "once to start, once for the resume")
        self.assertEqual(message, f"Bearer {ADMIN_TOKEN}", "the later turn travels with the token the resume obtained")
        self.assertEqual(result["turns"], 2)

    def test_the_resume_keeps_the_conversation_and_the_channel(self) -> None:
        lc = with_timeline(plain_case(), ["session_resume:same_channel"], turns=["The one ending 1234"])
        with Rig() as rig:
            result = run_case(rig.env, lc)
            paths = [path for method, path, _, _ in rig.noema.requests if method == "POST"]
        self.assertEqual(result["status"], "COMPLETED", result["error"])
        self.assertEqual(sum(1 for p in paths if p == "/v1/conversations"), 1, "no second conversation is started")
        self.assertIn("/v1/conversations/conv_0001/messages", paths)

    def test_a_resume_that_cannot_get_a_token_is_an_error_not_a_skip(self) -> None:
        lc = with_timeline(plain_case(), ["session_resume:same_channel"], turns=["The one ending 1234"])
        with Rig() as rig:
            calls = {"n": 0}
            real = rig.sessions.token_for

            def flaky(principal_id: str, roles: list[str]) -> str:
                calls["n"] += 1
                if calls["n"] >= 2 and principal_id == lc.case["principal"]["principal_id"]:
                    from benchmark.runner.env import EnvironmentUnavailable
                    raise EnvironmentUnavailable("session provider", "no token")
                return real(principal_id, roles)

            rig.sessions.token_for = flaky  # type: ignore[method-assign]
            result = run_case(rig.env, lc)
        self.assertEqual(result["status"], "ERROR")

    def test_another_resume_argument_is_unsupported(self) -> None:
        with Rig() as rig:
            result = run_case(rig.env, with_timeline(plain_case(), ["session_resume:other_channel"]))
            self.assertEqual(rig.noema.requests, [])
        self.assertEqual(result["error"]["code"], "EVENT_KIND_UNSUPPORTED")

    def test_a_resume_at_the_start_of_the_timeline_is_an_error_not_a_drop(self) -> None:
        with Rig() as rig:
            result = run_case(rig.env, with_timeline(plain_case(), ["session_resume:same_channel"], at="+00:00:00"))
        self.assertEqual((result["status"], result["error"]["code"]), ("ERROR", "EVENT_AT_START_UNSUPPORTED"))


class Support(unittest.TestCase):
    def test_the_support_report_says_a_webhook_case_is_blocked_until_a_sender_is_configured(self) -> None:
        from benchmark.runner.support import reasons_for
        lc = with_timeline(plain_case(), ["webhook:settlement_updated", "session_resume:same_channel"])
        self.assertEqual(reasons_for(lc), {"event: webhook"})
        self.assertEqual(reasons_for(lc, webhooks=True), set())
        self.assertEqual(reasons_for(with_timeline(plain_case(), ["session_resume:same_channel"])), set())


if __name__ == "__main__":
    unittest.main()
