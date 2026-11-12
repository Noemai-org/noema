"""The Go test shard splitter (tools/ci/go_shards.py) used by the core workflow: every package is in exactly one shard, the split is deterministic, and it is balanced by the
measured seconds in go_shard_weights.json, not by count. A package that is missing from a shard would silently not be tested, so coverage is the property that matters."""
import json
import subprocess
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import go_shards  # noqa: E402

PREFIX = "github.com/Noemai-org/noema/services/core/"
PKGS = [PREFIX + p for p in ("record/chain", "platform/db", "record/reconstruct", "trust/grants", "outcome/store", "evidence/layer", "execution/controller", "execution/e2e",
                             "adapters", "adapters/outcome_e2e", "trust/decide", "migrations", "edge", "cmd/core", "workflows", "orchestrator")]


class Shards(unittest.TestCase):
    def test_every_package_is_in_exactly_one_shard(self) -> None:
        for n in (1, 2, 3, 4):
            seen = [p for i in range(n) for p in go_shards.shard(PKGS, n, i, {})]
            self.assertEqual(sorted(seen), sorted(PKGS), f"{n} shards")

    def test_the_split_is_deterministic_and_independent_of_input_order(self) -> None:
        a = [go_shards.shard(PKGS, 3, i, {PREFIX + "record/chain": 49}) for i in range(3)]
        b = [go_shards.shard(list(reversed(PKGS)), 3, i, {PREFIX + "record/chain": 49}) for i in range(3)]
        self.assertEqual(a, b)

    def test_the_heaviest_packages_are_spread_not_stacked(self) -> None:
        weights = {PREFIX + "record/chain": 49, PREFIX + "platform/db": 40, PREFIX + "record/reconstruct": 23, PREFIX + "trust/grants": 19}
        shards = [go_shards.shard(PKGS, 3, i, weights) for i in range(3)]
        for heavy in ("record/chain", "platform/db", "record/reconstruct"):
            holders = [i for i, s in enumerate(shards) if PREFIX + heavy in s]
            self.assertEqual(len(holders), 1)
        self.assertEqual(len({next(i for i, s in enumerate(shards) if PREFIX + h in s) for h in ("record/chain", "platform/db", "record/reconstruct")}), 3)

    def test_the_busiest_shard_is_within_a_third_of_the_ideal_on_the_real_weights(self) -> None:
        weights = json.loads((HERE / "go_shard_weights.json").read_text())["seconds"]
        pkgs = [PREFIX + p for p in weights]
        shards = [go_shards.shard(pkgs, 3, i, weights) for i in range(3)]
        loads = [sum(weights.get(p, 2) for p in s) for s in shards]
        # a shard cannot finish before its heaviest package, so the bound is the larger of that and a third of the total plus a third
        self.assertLessEqual(max(loads), max(sum(loads) / 3 * 4 / 3, max(weights.values())), loads)
        db = [i for i, sh in enumerate(shards) if PREFIX + "platform/db" in sh]
        self.assertEqual(len(db), 1)
        self.assertLessEqual(loads[db[0]], max(weights.values()) + 30, "the heaviest package should share its shard with little else")

    def test_the_command_line_prints_one_shards_packages(self) -> None:
        out = subprocess.run([sys.executable, "-I", str(HERE / "go_shards.py"), "--shards", "2", "--index", "1", "--weights", str(HERE / "go_shard_weights.json")],
                             input="\n".join(PKGS) + "\n", capture_output=True, text=True, check=True).stdout.split()
        self.assertEqual(sorted(out), sorted(go_shards.shard(PKGS, 2, 1, json.loads((HERE / "go_shard_weights.json").read_text())["seconds"])))

    def test_bad_arguments_fail(self) -> None:
        for args in (["--shards", "0", "--index", "0"], ["--shards", "2", "--index", "2"], ["--shards", "2", "--index", "-1"]):
            r = subprocess.run([sys.executable, "-I", str(HERE / "go_shards.py"), *args], input="a\n", capture_output=True, text=True)
            self.assertNotEqual(r.returncode, 0, args)


if __name__ == "__main__":
    unittest.main()
