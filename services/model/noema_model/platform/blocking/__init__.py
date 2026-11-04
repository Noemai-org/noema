"""A bounded pool for the blocking calls of the model service (UND-12; REQ-TST-010, REQ-NFR-002; INV-16).

The standard library's HTTP client blocks, so each call runs on a worker thread, never on the event loop. The pool is the
whole of that machinery, and it has three properties the shared default executor does not:

- It is bounded. At most `workers` calls run at once, and a call that finds every worker busy is refused at once with
  `PoolSaturated`. There is no queue: nothing waits for a slot, and nothing is sent that was refused.
- A slot is taken for as long as its thread runs. A thread cannot be cancelled, so a call whose caller timed out still holds
  its slot until the thread returns; saturation is therefore reported as it really is.
- A slot is released when the thread returns, when the call raises, and when the call is cancelled before it started.

Each transport owns its own pool, so one transport being full never refuses the other's calls.
"""

import asyncio
import threading
from collections.abc import Callable
from concurrent.futures import Future, ThreadPoolExecutor

from noema_model.platform.config import ConfigError


class PoolSaturated(Exception):
    """Every worker is busy. The call was refused at once; nothing was sent. It carries no request data."""


class BoundedBlockingPool:
    def __init__(self, workers: int, *, name: str) -> None:
        if isinstance(workers, bool) or not isinstance(workers, int) or workers < 1:
            raise ConfigError(f"{name}: the pool needs an integer number of workers of at least 1")
        self._workers = workers
        self._executor = ThreadPoolExecutor(max_workers=workers, thread_name_prefix=f"noema-{name}")
        self._lock = threading.Lock()
        self._busy = 0

    @property
    def workers(self) -> int:
        return self._workers

    def _take(self) -> None:
        with self._lock:
            if self._busy >= self._workers:
                raise PoolSaturated
            self._busy += 1

    def _release(self, _future: object = None) -> None:
        with self._lock:
            self._busy -= 1

    async def run[T](self, call: Callable[[], T]) -> T:
        """Run `call` on a worker thread and return its result. Raise PoolSaturated, without waiting, when no worker is free.

        Cancelling the awaiting task stops the wait; it does not stop a thread that is already running, which keeps its slot
        until it returns."""
        self._take()
        try:
            future: Future[T] = self._executor.submit(call)
        except BaseException:
            self._release()
            raise
        # Runs when the call finishes, raises, or is cancelled before it started: the slot is never leaked.
        future.add_done_callback(self._release)
        return await asyncio.wrap_future(future)

    def close(self) -> None:
        self._executor.shutdown(wait=False, cancel_futures=True)
