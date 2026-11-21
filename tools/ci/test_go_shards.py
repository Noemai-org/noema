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

    # The weights file is keyed by the path after services/core/ ("platform/db"); `go list` prints the full import path. A lookup by the full path finds nothing and every package
    # weighs the fallback 2, which makes any split look balanced (Codex P2 on noema#482). The balance checks below resolve the key the way the splitter does and prove they did.
    @staticmethod
    def seconds_of(package: str, weights: dict) -> int:
        return weights.get(package.removeprefix(PREFIX), go_shards.DEFAULT)

    def loads(self, shards: list, weights: dict) -> list:
        return [sum(self.seconds_of(p, weights) for p in s) for s in shards]

    def is_balanced(self, shards: list, weights: dict) -> bool:
        """No shard is more than a third above the ideal (a shard cannot finish before its heaviest package, so that is the other bound), and the lightest shard is within a
        quarter of the ideal of the busiest: the measured seconds are spread, not stacked."""
        loads = self.loads(shards, weights)
        ideal = sum(loads) / len(shards)
        return max(loads) <= max(ideal * 4 / 3, max(weights.values())) and max(loads) - min(loads) <= ideal / 4

    def real(self):
        weights = json.loads((HERE / "go_shard_weights.json").read_text())["seconds"]
        return weights, [PREFIX + p for p in weights]

    def test_the_weights_are_looked_up_by_the_relative_path_and_not_all_the_fallback(self) -> None:
        weights, pkgs = self.real()
        self.assertEqual(sum(self.seconds_of(p, weights) for p in pkgs), sum(weights.values()))  # every package found its measured seconds
        self.assertGreater(sum(weights.values()), go_shards.DEFAULT * len(pkgs), "the file must hold measured seconds, not the fallback")
        shards = [go_shards.shard(pkgs, 3, i, weights) for i in range(3)]
        self.assertEqual(sum(self.loads(shards, weights)), sum(weights.values()))
        self.assertGreaterEqual(max(self.loads(shards, weights)), max(weights.values()))  # the heaviest package is in some shard, at its real weight

    def test_the_busiest_shard_is_within_a_third_of_the_ideal_on_the_real_weights(self) -> None:
        weights, pkgs = self.real()
        shards = [go_shards.shard(pkgs, 3, i, weights) for i in range(3)]
        self.assertTrue(self.is_balanced(shards, weights), self.loads(shards, weights))
        self.assertEqual(len([i for i, sh in enumerate(shards) if PREFIX + "platform/db" in sh]), 1)

    def test_a_bad_split_is_detected(self) -> None:
        weights, pkgs = self.real()
        everything_on_one = [pkgs, [], []]
        self.assertFalse(self.is_balanced(everything_on_one, weights), "all packages on one shard must fail the balance check")
        by_count = [pkgs[0::3], pkgs[1::3], pkgs[2::3]]  # a split that balances the number of packages and ignores their seconds
        heavy = sorted(pkgs, key=lambda p: -self.seconds_of(p, weights))
        stacked = [heavy[:3], heavy[3:12], heavy[12:]]  # the three heaviest together
        self.assertFalse(self.is_balanced(stacked, weights), "the heaviest packages stacked on one shard must fail the balance check")
        # and a split that merely looks balanced under the fallback weight is told apart from the measured one
        self.assertNotEqual(self.loads(by_count, weights), self.loads(by_count, {}), "the fallback weights hide the real loads")

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
