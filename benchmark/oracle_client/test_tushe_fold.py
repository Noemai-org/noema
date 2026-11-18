"""BEN-03 follow-up (the M-02 fold; PLAN-tushe-readonly-fold.md section 3.2). Written before `tushe_fold.py` and seen failing. SYNTHETIC rows. No route, no field name of Tushe's is asserted:
the row shape is a parameter, and the default names are ASSUMED until a delta file adopts the addendum (AGENTS section 2, item 4)."""
import itertools
import unittest
from typing import Any

from benchmark.oracle_client.tushe_fold import ASSUMED_SHAPE, Shape, fold_transitions

Json = dict[str, Any]


def row(i: int, at: str, frm: str, to: str, seq: int | None = None) -> Json:
    out: Json = {"id": f"t{i}", "occurred_at": at, "from_state": frm, "to_state": to}
    if seq is not None:
        out["seq"] = seq
    return out


CHAIN = [row(1, "2026-11-01T10:00:00Z", "CREATED", "PENDING"), row(2, "2026-11-01T10:05:00Z", "PENDING", "SETTLING"), row(3, "2026-11-01T10:09:00Z", "SETTLING", "SETTLED")]


class Fold(unittest.TestCase):
    def test_the_state_is_the_last_transition_at_or_before_the_time(self) -> None:
        for at, state, used in (("2026-11-01T10:00:00Z", "PENDING", ["t1"]), ("2026-11-01T10:07:00Z", "SETTLING", ["t1", "t2"]), ("2026-11-01T10:09:00Z", "SETTLED", ["t1", "t2", "t3"]),
                                ("2026-11-02T00:00:00Z", "SETTLED", ["t1", "t2", "t3"])):
            got = fold_transitions(CHAIN, at)
            self.assertEqual(("OK", state, used), (got.status, got.state, list(got.used)), at)

    def test_before_the_first_transition_the_payout_does_not_exist_yet_and_there_is_no_state(self) -> None:
        got = fold_transitions(CHAIN, "2026-11-01T09:59:59Z")
        self.assertEqual(("NOT_YET_EXISTING", None, ()), (got.status, got.state, got.used))
        self.assertEqual("NOT_YET_EXISTING", fold_transitions([], "2026-11-01T10:00:00Z").status)

    def test_input_order_never_matters_for_every_permutation(self) -> None:
        for at in ("2026-11-01T10:04:00Z", "2026-11-01T10:09:00Z"):
            expected = fold_transitions(CHAIN, at)
            for perm in itertools.permutations(CHAIN):
                got = fold_transitions(list(perm), at)
                self.assertEqual((expected.status, expected.state, expected.used), (got.status, got.state, got.used), (at, [r["id"] for r in perm]))

    def test_the_prefix_property_the_answer_at_a_time_ignores_everything_later_even_a_broken_row(self) -> None:
        broken_later = [*CHAIN, row(4, "2026-11-03T00:00:00Z", "NOT_THE_LAST_STATE", "X")]
        for at in ("2026-11-01T10:00:00Z", "2026-11-01T10:07:00Z", "2026-11-01T10:09:00Z"):
            self.assertEqual(fold_transitions(CHAIN, at), fold_transitions(broken_later, at), at)
        self.assertEqual("CHAIN_BROKEN", fold_transitions(broken_later, "2026-11-03T00:00:00Z").status)

    def test_a_gap_or_a_branch_in_the_chain_gives_no_state(self) -> None:
        gap = [CHAIN[0], row(3, "2026-11-01T10:09:00Z", "SETTLING", "SETTLED")]
        branch = [CHAIN[0], CHAIN[1], row(9, "2026-11-01T10:06:00Z", "PENDING", "FAILED")]
        for rows in (gap, branch):
            got = fold_transitions(rows, "2026-11-02T00:00:00Z")
            self.assertEqual(("CHAIN_BROKEN", None), (got.status, got.state))

    def test_two_transitions_at_one_instant_with_no_sequence_are_ambiguous_and_a_sequence_orders_them(self) -> None:
        tied = [row(1, "2026-11-01T10:00:00Z", "CREATED", "PENDING"), row(2, "2026-11-01T10:00:00Z", "PENDING", "SETTLED")]
        got = fold_transitions(tied, "2026-11-01T10:00:00Z")
        self.assertEqual(("ORDER_AMBIGUOUS", None), (got.status, got.state))
        seq = Shape(seq="seq")
        ordered = [row(1, "2026-11-01T10:00:00Z", "CREATED", "PENDING", 1), row(2, "2026-11-01T10:00:00Z", "PENDING", "SETTLED", 2)]
        for perm in itertools.permutations(ordered):
            got = fold_transitions(list(perm), "2026-11-01T10:00:00Z", seq)
            self.assertEqual(("OK", "SETTLED"), (got.status, got.state))
        same = [row(1, "2026-11-01T10:00:00Z", "CREATED", "PENDING", 1), row(2, "2026-11-01T10:00:00Z", "PENDING", "SETTLED", 1)]
        self.assertEqual("ORDER_AMBIGUOUS", fold_transitions(same, "2026-11-01T10:00:00Z", seq).status)

    def test_a_tie_after_the_time_does_not_matter(self) -> None:
        later_tie = [*CHAIN, row(4, "2026-11-05T00:00:00Z", "SETTLED", "A"), row(5, "2026-11-05T00:00:00Z", "A", "B")]
        self.assertEqual("OK", fold_transitions(later_tie, "2026-11-02T00:00:00Z").status)

    def test_an_unreadable_row_anywhere_fails_closed_because_its_time_is_unknown(self) -> None:
        bad_rows: list[Json] = [{**CHAIN[2], "occurred_at": "yesterday"}, {k: v for k, v in CHAIN[2].items() if k != "to_state"}, {**CHAIN[2], "id": ""}, {**CHAIN[2], "from_state": 3},
                                {**CHAIN[2], "occurred_at": "2026-11-01T10:09:00"}]  # no zone: not an instant
        for bad in bad_rows:
            got = fold_transitions([CHAIN[0], CHAIN[1], bad], "2026-11-01T10:01:00Z")
            self.assertEqual(("UNREADABLE", None), (got.status, got.state), bad)
        for at in ("soon", "2026-11-01T10:01:00", "", None):
            self.assertEqual("UNREADABLE", fold_transitions(CHAIN, at).status, at)
        for not_rows in (None, "rows", [None], [[1]], {"id": "t1"}):
            self.assertEqual("UNREADABLE", fold_transitions(not_rows, "2026-11-01T10:01:00Z").status, not_rows)

    def test_a_missing_or_non_integer_sequence_is_unreadable_when_a_sequence_is_declared(self) -> None:
        seq = Shape(seq="seq")
        for bad in (CHAIN[0], {**CHAIN[0], "seq": "1"}, {**CHAIN[0], "seq": True}):
            self.assertEqual("UNREADABLE", fold_transitions([bad], "2026-11-02T00:00:00Z", seq).status, bad)

    def test_an_identical_duplicate_row_is_collapsed_and_said_a_conflicting_one_is_broken(self) -> None:
        got = fold_transitions([*CHAIN, dict(CHAIN[1])], "2026-11-02T00:00:00Z")
        self.assertEqual(("OK", "SETTLED", 1), (got.status, got.state, got.collapsed))
        conflicting = [*CHAIN, {**CHAIN[1], "to_state": "ELSEWHERE"}]
        self.assertEqual("CHAIN_BROKEN", fold_transitions(conflicting, "2026-11-02T00:00:00Z").status)

    def test_offsets_are_instants_not_strings(self) -> None:
        shifted = [row(1, "2026-11-01T11:00:00+01:00", "CREATED", "PENDING"), row(2, "2026-11-01T09:05:00Z", "PENDING", "SETTLING")]
        got = fold_transitions(shifted, "2026-11-01T10:30:00+01:00")  # 09:30Z
        self.assertEqual(("OK", "SETTLING"), (got.status, got.state))

    def test_the_inputs_are_not_changed_and_the_result_is_immutable(self) -> None:
        rows = [dict(r) for r in CHAIN]
        before = [dict(r) for r in rows]
        got = fold_transitions(rows, "2026-11-02T00:00:00Z")
        self.assertEqual(before, rows)
        with self.assertRaises(Exception):
            setattr(got, "state", "X")

    def test_only_ok_yields_a_state_and_the_assumed_shape_is_named_as_assumed(self) -> None:
        for rows in ([], CHAIN[:1], [CHAIN[0], CHAIN[2]]):
            got = fold_transitions(rows, "2026-11-02T00:00:00Z")
            self.assertEqual(got.status == "OK", got.state is not None)
        self.assertTrue(ASSUMED_SHAPE.assumed)
        self.assertIsNone(ASSUMED_SHAPE.seq)


if __name__ == "__main__":
    unittest.main()
