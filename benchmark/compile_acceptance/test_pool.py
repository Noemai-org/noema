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


def fan_out_of(source: str) -> int:
    """The calls the pool may have to serve for one pipeline: the SUM, over every `asyncio.gather` in `run_pipeline`, of the calls it starts (an argument that is a bare name is a task or a coroutine that
    already exists, not a new call), or the tasks `create_task` makes, whichever is larger (A480, noema#832 follow-up: sum all gathers, not the widest; the real pipeline's is 5, against a pool of 6). A gather
    with a starred argument has no readable width and a pipeline with no gather has none to read: both are refused, so a new shape is taught on purpose."""
    module = ast.parse(source)
    (run_pipeline,) = [n for n in module.body if isinstance(n, ast.AsyncFunctionDef) and n.name == "run_pipeline"]
    gathers = _calls_named(run_pipeline, "asyncio", "gather")
    if not gathers or any(isinstance(a, ast.Starred) for g in gathers for a in g.args):
        raise AssertionError("run_pipeline must have at least one asyncio.gather and every one with listed awaitables, or this test must be taught the new shape")
    started = sum(1 for g in gathers for a in g.args if not isinstance(a, ast.Name))
    return max(started, len(_calls_named(run_pipeline, "asyncio", "create_task")))


def pipeline_fan_out() -> int:
    return fan_out_of(RUN_PY.read_text(encoding="utf-8"))


SYNTHETIC_FIVE_AND_ONE = """
async def run_pipeline():
    gathered = await asyncio.gather(a(), b(), c(), d(), e())
    screening = asyncio.create_task(f())
    screened = (await asyncio.gather(screening, return_exceptions=True))[0]
"""


class FanOutReading(unittest.TestCase):
    """noema#792 added a second `asyncio.gather` to `run_pipeline` (the candidates' screening, one awaitable), which the reader rejected, so main's benchmark job went red. The fan-out is the WIDEST
    gather, not the only one; a gather with a starred argument has no readable width and is still refused, and a pipeline with no gather is refused."""

    def test_a_gather_of_an_existing_task_adds_nothing_when_the_task_is_counted_once(self) -> None:
        # The real shape: five calls in the first gather, then one task made by create_task and awaited by a second gather. The sum of the CALLS the gathers start is 5; the awaited task is the one
        # create_task counts, not a sixth call (A480: the sum is 5, against a pool of 6).
        self.assertEqual(fan_out_of(SYNTHETIC_FIVE_AND_ONE), 5)

    def test_the_fan_out_is_the_sum_of_all_gathers_not_the_widest(self) -> None:
        # A480 (noema#832 follow-up): two gathers of calls, three and two, are five calls the pool may have to serve, not three.
        self.assertEqual(fan_out_of("async def run_pipeline():\n    await asyncio.gather(a(), b(), c())\n    await asyncio.gather(d(), e())\n"), 5)
        self.assertEqual(fan_out_of("async def run_pipeline():\n    await asyncio.gather(a())\n    await asyncio.gather(b())\n    await asyncio.gather(c())\n"), 3)

    def test_a_gather_with_a_starred_argument_is_refused(self) -> None:
        with self.assertRaises(AssertionError):
            fan_out_of("async def run_pipeline():\n    await asyncio.gather(*tasks)\n")

    def test_a_pipeline_with_no_gather_is_refused(self) -> None:
        with self.assertRaises(AssertionError):
            fan_out_of("async def run_pipeline():\n    await a()\n")

    def test_created_tasks_count_when_they_outnumber_every_gather(self) -> None:
        self.assertEqual(fan_out_of("async def run_pipeline():\n    await asyncio.gather(a())\n    x = asyncio.create_task(b())\n    y = asyncio.create_task(c())\n"), 2)


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
