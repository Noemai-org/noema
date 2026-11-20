"""UND-16: the acceptance run's transport pool covers the pipeline's concurrent fan-out (REQ-INT-002, REQ-TST-010; INV-16).

Run 44521380842: the pipeline starts five calls together (the node's enumerated slots, the flags, the locale, the slot mentions and the quiet injection screen) and the pool held four workers.
`BoundedBlockingPool` refuses a call at once when no worker is free, so the fifth call, the screen, was refused (UNAVAILABLE, no status) and the screen was silently empty. The pool size is a run
setting, not a safety threshold. The fan-out is read from the pipeline's source, not imported: the benchmark environment does not install the model service. SYNTHETIC: no call is made."""
import ast
import unittest
from pathlib import Path

from benchmark.compile_acceptance import build

RUN_PY = Path(__file__).resolve().parents[2] / "services" / "model" / "noema_model" / "understanding" / "pipeline" / "run.py"
# A worker thread cannot be cancelled: a call whose caller timed out keeps its slot until the thread returns (platform/blocking). One spare covers one such call.
SPARE = 1


def _calls_named(tree: ast.AST, owner: str, name: str) -> list[ast.Call]:
    return [n for n in ast.walk(tree) if isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute) and n.func.attr == name
            and isinstance(n.func.value, ast.Name) and n.func.value.id == owner]


def pipeline_fan_out() -> int:
    """The most calls the pipeline has in flight at once: the width of the one `asyncio.gather` in `run_pipeline`, or the tasks it creates, whichever is larger."""
    module = ast.parse(RUN_PY.read_text(encoding="utf-8"))
    (run_pipeline,) = [n for n in module.body if isinstance(n, ast.AsyncFunctionDef) and n.name == "run_pipeline"]
    gathers = _calls_named(run_pipeline, "asyncio", "gather")
    if len(gathers) != 1 or any(isinstance(a, ast.Starred) for a in gathers[0].args):
        raise AssertionError("run_pipeline must have exactly one asyncio.gather with listed awaitables, or this test must be taught the new shape")
    return max(len(gathers[0].args), len(_calls_named(run_pipeline, "asyncio", "create_task")))


class PoolCoversFanOut(unittest.TestCase):
    def test_the_fan_out_is_read_from_the_pipeline_and_is_five(self) -> None:
        """The five starts of run.py step 2. If the pipeline changes this number, the number here changes with it, on purpose."""
        self.assertEqual(pipeline_fan_out(), 5)

    def test_the_pool_is_an_integer_of_at_least_the_fan_out_plus_one_spare(self) -> None:
        self.assertIs(type(build.POOL_SIZE), int)
        self.assertGreaterEqual(build.POOL_SIZE, pipeline_fan_out() + SPARE)

    def test_the_pool_is_no_larger_than_needed(self) -> None:
        """A bigger pool is not a safety gain, only more calls in flight on the provider (a run setting, and the smallest that passes)."""
        self.assertEqual(build.POOL_SIZE, pipeline_fan_out() + SPARE)


if __name__ == "__main__":
    unittest.main()
