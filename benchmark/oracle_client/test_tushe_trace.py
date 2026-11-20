"""BEN-03 follow-up (the AT-19 trace join; PLAN-tushe-readonly-fold.md section 3.3). Written before `tushe_trace.py` and seen failing. SYNTHETIC rows. The member names are a parameter
(ASSUMED until a delta file adopts the addendum): the logic is what Noema requires of whatever Tushe returns, not a claim about Tushe."""
import unittest
from typing import Any

from benchmark.oracle_client.tushe_trace import ASSUMED_SHAPE, TraceShape, join_trace

Json = dict[str, Any]
TRANSITION: Json = {"id": "t2", "trace_id": "tr_1", "to_state": "SETTLING", "occurred_at": "2026-11-01T10:05:00Z"}
EVENT: Json = {"message_id": "m1", "trace_id": "tr_1", "actor": "svc_payout", "request": "req_9", "correlation": "cor_4"}


class Join(unittest.TestCase):
    def test_one_event_with_all_five_elements_is_complete(self) -> None:
        got = join_trace(TRANSITION, [EVENT, {**EVENT, "message_id": "m2", "trace_id": "tr_other"}])
        self.assertEqual(("COMPLETE", (), 0), (got.status, got.missing, got.collapsed))

    def test_no_event_for_the_trace_is_audit_incomplete(self) -> None:
        for events in ([], [{**EVENT, "trace_id": "tr_other"}]):
            self.assertEqual("NO_EVENT", join_trace(TRANSITION, events).status)

    def test_two_distinct_events_make_the_join_ambiguous(self) -> None:
        got = join_trace(TRANSITION, [EVENT, {**EVENT, "message_id": "m2", "actor": "someone_else"}])
        self.assertEqual("AMBIGUOUS_JOIN", got.status)

    def test_a_duplicate_delivery_is_collapsed_and_reported_not_hidden(self) -> None:
        got = join_trace(TRANSITION, [EVENT, dict(EVENT), dict(EVENT)])
        self.assertEqual(("COMPLETE", 2), (got.status, got.collapsed))

    def test_the_same_message_identity_with_different_content_is_ambiguous(self) -> None:
        self.assertEqual("AMBIGUOUS_JOIN", join_trace(TRANSITION, [EVENT, {**EVENT, "actor": "other"}]).status)

    def test_events_with_no_message_identity_cannot_be_collapsed(self) -> None:
        a = {k: v for k, v in EVENT.items() if k != "message_id"}
        self.assertEqual("AMBIGUOUS_JOIN", join_trace(TRANSITION, [a, dict(a)]).status)

    def test_each_missing_element_is_named_and_none_is_a_pass(self) -> None:
        for element, where in (("actor", "event"), ("request", "event"), ("correlation", "event"), ("to_state", "transition"), ("occurred_at", "transition")):
            transition, event = dict(TRANSITION), dict(EVENT)
            (transition if where == "transition" else event)[element] = ""
            got = join_trace(transition, [event])
            self.assertEqual("INCOMPLETE", got.status, element)
            self.assertEqual((element,), got.missing)
            event2 = {k: v for k, v in EVENT.items() if k != element}
            if where == "event":
                self.assertEqual((element,), join_trace(TRANSITION, [event2]).missing)

    def test_null_blank_and_empty_collections_count_as_missing(self) -> None:
        empties: list[Any] = [None, "", "   ", [], {}]
        for empty in empties:
            got = join_trace(TRANSITION, [{**EVENT, "request": empty}])
            self.assertEqual(("INCOMPLETE", ("request",)), (got.status, got.missing), repr(empty))

    def test_several_missing_elements_are_all_named_in_a_stable_order(self) -> None:
        got = join_trace({**TRANSITION, "occurred_at": None}, [{**EVENT, "actor": "", "correlation": None}])
        self.assertEqual(("INCOMPLETE", ("actor", "correlation", "occurred_at")), (got.status, got.missing))

    def test_a_transition_without_a_trace_key_cannot_be_joined(self) -> None:
        for bad in ({k: v for k, v in TRANSITION.items() if k != "trace_id"}, {**TRANSITION, "trace_id": ""}, {**TRANSITION, "trace_id": 7}):
            got = join_trace(bad, [EVENT])
            self.assertEqual(("INCOMPLETE", ("trace_id",)), (got.status, got.missing), bad)

    def test_malformed_input_is_unreadable_never_complete(self) -> None:
        for transition, events in ((None, [EVENT]), (TRANSITION, None), (TRANSITION, "events"), (TRANSITION, [None]), (TRANSITION, [[1]]), ([1], [EVENT])):
            self.assertEqual("UNREADABLE", join_trace(transition, events).status, (transition, events))

    def test_the_member_names_are_a_parameter_and_the_defaults_are_assumed(self) -> None:
        shape = TraceShape(transition_key="tid", event_key="tid", message_id="mid", actor="who", request="what", correlation="corr", state="st", timestamp="ts")
        transition = {"tid": "x", "st": "S", "ts": "T"}
        event = {"mid": "m", "tid": "x", "who": "a", "what": "r", "corr": "c", "corr_wrong": "c"}
        self.assertEqual("INCOMPLETE", join_trace(transition, [{k: v for k, v in event.items() if k != "corr"}], shape).status)
        self.assertEqual("COMPLETE", join_trace(transition, [event], shape).status)
        self.assertTrue(ASSUMED_SHAPE.assumed)

    def test_the_inputs_are_not_changed(self) -> None:
        transition, events = dict(TRANSITION), [dict(EVENT), dict(EVENT)]
        join_trace(transition, events)
        self.assertEqual((TRANSITION, [EVENT, EVENT]), (transition, events))


if __name__ == "__main__":
    unittest.main()
