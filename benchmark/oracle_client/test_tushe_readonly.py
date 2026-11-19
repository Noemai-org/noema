"""BEN-03 / AT-05, AT-14, AT-17 (PLAN-tushe-readonly-fold.md section 3.4): the harness's read-only reader of Tushe's two reads, against a local SYNTHETIC stand-in. Written before
`tushe_readonly.py` and seen failing.

The stand-in is a loopback server that speaks delta v1.3's routes (`GET /v1/events?after=&limit=`, `GET /v1/accounts/{a}/payouts/{p}/transitions`) with made-up rows. Nothing here was observed
against Tushe (AGENTS section 2, item 4): the envelope of each list and how the key is presented are REQUIRED parameters with no default, so adopting the real ones changes one value in one place.
The reader sends GET only, never retries, never follows a redirect, never puts the key in an error, and never turns a failure into "no rows"."""
import contextlib
import http.server
import json
import tempfile
import threading
import time
import unittest
from collections.abc import Callable, Iterator
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlsplit

from benchmark.oracle_client.tushe_readonly import TusheReadError, TusheReadOnly

Json = dict[str, Any]
KEY = "syn-readonly-key-0123456789abcdef"
EVENTS = [{"id": f"syn-evt-{n}", "sequence": s, "traceId": t, "type": "payout.step"} for n, (s, t) in enumerate([(10, "t1"), (11, "t1"), (15, "t2"), (16, "t2"), (20, "t3")], start=1)]
TRANSITIONS = [
    {"seq": 1, "lifecycle": "payout", "fromState": None, "toState": "CREATED", "source": "api", "sourceRef": "s1", "inferred": False, "injected": False, "reason": "created",
     "occurredAt": "2026-11-01T10:00:00Z", "observedAt": "2026-11-01T10:00:01Z", "actor": "a1", "request": {"id": "r1", "idempotencyKey": "k1"}},
    {"seq": 2, "lifecycle": "payout", "fromState": "CREATED", "toState": "PENDING", "source": "api", "sourceRef": "s2", "inferred": False, "injected": False, "reason": "pending",
     "occurredAt": "2026-11-01T10:05:00Z", "observedAt": "2026-11-01T10:05:01Z", "actor": "a1", "request": {"id": "r2", "idempotencyKey": "k2"}},
    {"seq": 3, "lifecycle": "payout", "fromState": "PENDING", "toState": "SETTLED", "source": "processor", "sourceRef": "s3", "inferred": True, "injected": False, "reason": "settled",
     "occurredAt": "2026-11-01T10:09:00Z", "observedAt": "2026-11-01T10:09:01Z", "actor": "p1", "request": {"id": "r3", "idempotencyKey": "k3"}},
]
TRANSITIONS_PATH = "/v1/accounts/acc_1/payouts/pay_1/transitions"

Handler = Callable[[str, str, dict[str, list[str]], dict[str, str]], tuple[int, Any, dict[str, str]]]
Seen = list[tuple[str, str, dict[str, str]]]


def feed_handler(events: list[Json], key_name: str | None = "events") -> Handler:
    """A well-behaved stand-in: the transition log and the feed, with the feed paged on `sequence`."""
    def handle(method: str, path: str, query: dict[str, list[str]], headers: dict[str, str]) -> tuple[int, Any, dict[str, str]]:
        if path == TRANSITIONS_PATH:
            return 200, TRANSITIONS if key_name is None else {"transitions": TRANSITIONS}, {}
        if path == "/v1/events":
            after, limit = int(query["after"][0]), int(query["limit"][0])
            page = [e for e in events if e["sequence"] > after][:limit]
            return 200, page if key_name is None else {key_name: page}, {}
        return 404, {"error": {"code": "NOT_FOUND", "message": "no such route"}}, {}
    return handle


def fixed(body: Any, status: int = 200) -> Handler:
    """A stand-in that answers every request the same way."""
    def handle(method: str, path: str, query: dict[str, list[str]], headers: dict[str, str]) -> tuple[int, Any, dict[str, str]]:
        return status, body, {}
    return handle


@contextlib.contextmanager
def stand_in(handler: Handler) -> Iterator[tuple[str, Seen]]:
    seen: Seen = []

    class H(http.server.BaseHTTPRequestHandler):
        def _serve(self) -> None:
            parts = urlsplit(self.path)
            headers = {k.lower(): v for k, v in self.headers.items()}
            seen.append((self.command, self.path, headers))
            if self.command != "GET":
                self.send_response(405)
                self.end_headers()
                return
            status, body, extra = handler(self.command, parts.path, parse_qs(parts.query), headers)
            raw = body if isinstance(body, bytes) else json.dumps(body).encode()
            self.send_response(status)
            for k, v in extra.items():
                self.send_header(k, v)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = _serve

        def log_message(self, *args: Any) -> None:
            return

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
    server.daemon_threads = True
    thread = threading.Thread(target=lambda: server.serve_forever(poll_interval=0.01), daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_address[1]}", seen
    finally:
        server.shutdown()
        server.server_close()


@contextlib.contextmanager
def key_file() -> Iterator[Path]:
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "key"
        path.write_text(KEY + "\n", encoding="utf-8")
        yield path


def reader(base: str, key: Path, *, list_key: str | None = "events", present: str = "bearer", timeout: float = 2.0) -> TusheReadOnly:
    return TusheReadOnly(base, key, present=present, events_key=list_key, transitions_key=None if list_key is None else "transitions", loopback_http=True, timeout=timeout)


class Requests(unittest.TestCase):
    def test_it_sends_get_only_with_the_exact_paths_and_presents_the_key_from_the_file(self) -> None:
        with stand_in(feed_handler(EVENTS)) as (base, seen), key_file() as key:
            r = reader(base, key)
            r.transitions("acc_1", "pay_1")
            r.read_feed(after=0, limit=50, max_pages=2)
        self.assertEqual({"GET"}, {m for m, _, _ in seen})
        self.assertEqual(TRANSITIONS_PATH, seen[0][1])
        self.assertEqual("/v1/events?after=0&limit=50", seen[1][1])
        self.assertTrue(all(h.get("authorization") == f"Bearer {KEY}" for _, _, h in seen))

    def test_the_other_presentation_is_a_header_and_not_a_bearer(self) -> None:
        with stand_in(feed_handler(EVENTS)) as (base, seen), key_file() as key:
            reader(base, key, present="x-api-key").transitions("acc_1", "pay_1")
        self.assertEqual(KEY, seen[0][2].get("x-api-key"))
        self.assertNotIn("authorization", seen[0][2])

    def test_the_presentation_and_the_list_shapes_are_required_with_no_default(self) -> None:
        with key_file() as key:
            with self.assertRaises(TypeError):
                TusheReadOnly("https://x.invalid", key)  # type: ignore[call-arg]
            with self.assertRaises(TusheReadError) as caught:
                TusheReadOnly("https://x.invalid", key, present="cookie", events_key=None, transitions_key=None)
            self.assertEqual("INVALID", caught.exception.kind)

    def test_http_is_refused_unless_loopback_is_asked_for_and_the_host_is_loopback(self) -> None:
        with key_file() as key:
            for base, flag in (("http://127.0.0.1:1", False), ("http://example.invalid", True), ("ftp://x", True), ("example.invalid", False)):
                with self.assertRaises(TusheReadError, msg=base) as caught:
                    TusheReadOnly(base, key, present="bearer", events_key=None, transitions_key=None, loopback_http=flag)
                self.assertEqual("INVALID", caught.exception.kind)
            TusheReadOnly("https://example.invalid", key, present="bearer", events_key=None, transitions_key=None)  # https needs no flag, and no request is made

    def test_an_empty_or_missing_key_file_is_invalid_and_never_names_the_value(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            empty = Path(tmp) / "k"
            empty.write_text("\n", encoding="utf-8")
            for path in (empty, Path(tmp) / "absent"):
                with self.assertRaises(TusheReadError) as caught:
                    TusheReadOnly("https://x.invalid", path, present="bearer", events_key=None, transitions_key=None)
                self.assertEqual("INVALID", caught.exception.kind)

    def test_the_ids_are_checked_before_any_request_is_made(self) -> None:
        with stand_in(feed_handler(EVENTS)) as (base, seen), key_file() as key:
            r = reader(base, key)
            for bad in ("", "../x", "a b", "a/b", "a?b", "a" * 65, "é", "a\nb"):
                with self.assertRaises(TusheReadError, msg=repr(bad)) as caught:
                    r.transitions(bad, "pay_1")
                self.assertEqual("INVALID", caught.exception.kind)
                with self.assertRaises(TusheReadError):
                    r.transitions("acc_1", bad)
        self.assertEqual([], seen)


class Failures(unittest.TestCase):
    def read(self, status: int, body: Any = None, extra: dict[str, str] | None = None) -> tuple[TusheReadError, Seen]:
        with stand_in(lambda *_: (status, body if body is not None else {"error": {"code": "SOME_CODE", "message": "m"}}, extra or {})) as (base, seen), key_file() as key:
            with self.assertRaises(TusheReadError) as caught:
                reader(base, key).transitions("acc_1", "pay_1")
        return caught.exception, seen

    def test_each_failure_has_one_kind_and_none_of_them_is_no_rows(self) -> None:
        for status, kind in ((401, "REFUSED"), (403, "REFUSED"), (404, "NOT_FOUND"), (429, "UNAVAILABLE"), (500, "UNAVAILABLE"), (503, "UNAVAILABLE"), (400, "REFUSED")):
            error, seen = self.read(status)
            self.assertEqual(kind, error.kind, status)
            self.assertEqual(1, len(seen), f"{status}: no retry")

    def test_a_redirect_is_never_followed_and_is_refused(self) -> None:
        error, seen = self.read(302, {}, {"Location": "http://127.0.0.1:1/elsewhere"})
        self.assertEqual("REFUSED", error.kind)
        self.assertEqual(1, len(seen))

    def test_a_connection_that_cannot_be_made_and_a_timeout_are_unavailable(self) -> None:
        with key_file() as key:
            with self.assertRaises(TusheReadError) as refused:
                reader("http://127.0.0.1:1", key).transitions("acc_1", "pay_1")
        self.assertEqual("UNAVAILABLE", refused.exception.kind)

        def slow(*_: Any) -> tuple[int, Any, dict[str, str]]:
            time.sleep(1.0)
            return 200, [], {}
        with stand_in(slow) as (base, _), key_file() as key:
            with self.assertRaises(TusheReadError) as timed_out:
                reader(base, key, list_key=None, timeout=0.2).transitions("acc_1", "pay_1")
        self.assertEqual("UNAVAILABLE", timed_out.exception.kind)

    def test_a_body_that_cannot_be_read_is_malformed_never_an_empty_list(self) -> None:
        for body in (b"not json", b"", b"null", b'"x"', b'{"other": []}', b'{"transitions": {}}', b'{"transitions": ["x"]}', b"7"):
            error, _ = self.read(200, body)
            self.assertEqual("MALFORMED", error.kind, body)

    def test_neither_the_key_nor_the_servers_words_ever_appear_in_an_error(self) -> None:
        for status in (401, 404, 500):
            error, _ = self.read(status, {"error": {"code": f"X{KEY}", "message": f"the key {KEY} was wrong"}})
            self.assertNotIn(KEY, str(error))
            self.assertNotIn(KEY, repr(error))
            self.assertNotIn("was wrong", str(error))
        with stand_in(feed_handler(EVENTS)) as (base, _), key_file() as key:
            r = reader(base, key)
            self.assertNotIn(KEY, repr(r))
            self.assertNotIn(KEY, str(vars(r)) if hasattr(r, "__dict__") else "")

    def test_a_code_the_server_sends_is_kept_only_when_it_is_a_plain_code(self) -> None:
        plain, _ = self.read(404, {"error": {"code": "PAYOUT_NOT_FOUND", "message": "m"}})
        self.assertEqual("PAYOUT_NOT_FOUND", plain.code)
        odd, _ = self.read(404, {"error": {"code": "has spaces and a key", "message": "m"}})
        self.assertEqual("UNKNOWN", odd.code)


class Transitions(unittest.TestCase):
    def fetch(self, body: Any, list_key: str | None = None) -> list[Json]:
        with stand_in(lambda m, p, q, h: (200, body, {})) as (base, _), key_file() as key:
            return reader(base, key, list_key=list_key).transitions("acc_1", "pay_1")

    def test_a_bare_list_and_a_keyed_object_both_read_when_the_shape_says_so(self) -> None:
        self.assertEqual(TRANSITIONS, self.fetch(TRANSITIONS))
        self.assertEqual(TRANSITIONS, self.fetch({"transitions": TRANSITIONS}, list_key="events"))

    def test_the_shape_is_not_guessed(self) -> None:
        with self.assertRaises(TusheReadError) as caught:
            self.fetch({"transitions": TRANSITIONS}, list_key=None)
        self.assertEqual("MALFORMED", caught.exception.kind)
        with self.assertRaises(TusheReadError):
            self.fetch(TRANSITIONS, list_key="events")

    def test_an_empty_log_is_a_real_answer_only_because_the_read_succeeded(self) -> None:
        self.assertEqual([], self.fetch([]))

    def test_the_rows_must_strictly_rise_in_seq_and_carry_the_fields_the_fold_needs(self) -> None:
        def with_row(index: int, **change: Any) -> list[Json]:
            rows = [dict(r) for r in TRANSITIONS]
            rows[index].update(change)
            return rows
        bad: list[tuple[str, list[Json]]] = [
            ("a repeated seq", with_row(1, seq=1)), ("a falling seq", [TRANSITIONS[1], TRANSITIONS[0]]), ("a boolean seq", with_row(0, seq=True)), ("a text seq", with_row(0, seq="1")),
            ("no toState", with_row(0, toState="")), ("a numeric toState", with_row(0, toState=3)), ("a numeric fromState", with_row(1, fromState=2)),
            ("no occurredAt", with_row(0, occurredAt="")), ("a row that is not an object", [*TRANSITIONS, "x"])]  # type: ignore[list-item]
        for name, rows in bad:
            with self.assertRaises(TusheReadError, msg=name) as caught:
                self.fetch(rows)
            self.assertEqual("MALFORMED", caught.exception.kind, name)

    def test_fields_the_reader_does_not_need_pass_through_untouched(self) -> None:
        got = self.fetch(TRANSITIONS)
        self.assertEqual("a1", got[0]["actor"])
        self.assertEqual({"id": "r2", "idempotencyKey": "k2"}, got[1]["request"])


class Feed(unittest.TestCase):
    def feed(self, events: list[Json], *, after: int = 0, limit: int = 2, max_pages: int = 10, list_key: str | None = "events") -> tuple[list[Json], Seen]:
        with stand_in(feed_handler(events, key_name=list_key)) as (base, seen), key_file() as key:
            got = reader(base, key, list_key=list_key).read_feed(after=after, limit=limit, max_pages=max_pages)
        return got, list(seen)

    def test_it_pages_forward_on_the_sequence_never_on_a_timestamp(self) -> None:
        got, seen = self.feed(EVENTS)
        self.assertEqual(EVENTS, got)
        self.assertEqual(["/v1/events?after=0&limit=2", "/v1/events?after=11&limit=2", "/v1/events?after=16&limit=2"], [p for _, p, _ in seen])
        self.assertEqual(EVENTS[2:], self.feed(EVENTS, after=11)[0])

    def test_a_bare_list_reads_too_and_an_exactly_full_last_page_is_followed_by_one_more_request(self) -> None:
        got, seen = self.feed(EVENTS[:4], list_key=None)
        self.assertEqual(EVENTS[:4], got)
        self.assertEqual(3, len(seen))

    def test_an_empty_feed_is_empty_because_the_read_succeeded(self) -> None:
        self.assertEqual([], self.feed([])[0])

    def test_not_caught_up_within_the_page_limit_is_an_error_never_a_truncated_list(self) -> None:
        with self.assertRaises(TusheReadError) as caught:
            self.feed(EVENTS, max_pages=2)
        self.assertEqual("INVALID", caught.exception.kind)
        self.assertEqual("ADAPTER:FEED_PAGE_LIMIT", caught.exception.code)

    def test_a_sequence_that_does_not_strictly_rise_or_does_not_pass_the_cursor_is_malformed(self) -> None:
        cases: list[tuple[str, list[Json]]] = [
            ("a repeat inside a page", [EVENTS[0], {**EVENTS[0], "id": "x"}]), ("falling inside a page", [EVENTS[1], EVENTS[0]]),
            ("not past the cursor", [{**EVENTS[0], "sequence": 0}]), ("a boolean sequence", [{**EVENTS[0], "sequence": True}]), ("a text sequence", [{**EVENTS[0], "sequence": "10"}]),
            ("no id", [{**EVENTS[0], "id": ""}])]
        for name, events in cases:
            with stand_in(fixed({"events": events})) as (base, _), key_file() as key:
                with self.assertRaises(TusheReadError, msg=name) as caught:
                    reader(base, key).read_feed(after=0, limit=5, max_pages=3)
            self.assertEqual("MALFORMED", caught.exception.kind, name)

    def test_a_server_that_repeats_a_page_cannot_loop_the_reader(self) -> None:
        with stand_in(lambda m, p, q, h: (200, {"events": EVENTS[:2]}, {})) as (base, seen), key_file() as key:
            with self.assertRaises(TusheReadError) as caught:
                reader(base, key).read_feed(after=0, limit=2, max_pages=5)
        self.assertEqual("MALFORMED", caught.exception.kind)
        self.assertLessEqual(len(seen), 2)

    def test_the_arguments_are_checked_before_any_request(self) -> None:
        with stand_in(feed_handler(EVENTS)) as (base, seen), key_file() as key:
            r = reader(base, key)
            for kwargs in ({"after": -1, "limit": 2, "max_pages": 2}, {"after": 0, "limit": 0, "max_pages": 2}, {"after": 0, "limit": 2, "max_pages": 0}, {"after": True, "limit": 2, "max_pages": 2},
                           {"after": 0, "limit": "2", "max_pages": 2}, {"after": 0, "limit": 2, "max_pages": True}):
                with self.assertRaises(TusheReadError, msg=str(kwargs)) as caught:
                    r.read_feed(**kwargs)
                self.assertEqual("INVALID", caught.exception.kind)
        self.assertEqual([], seen)

    def test_events_for_a_trace_are_filtered_on_trace_id_in_sequence_order(self) -> None:
        with stand_in(feed_handler(EVENTS)) as (base, _), key_file() as key:
            got = reader(base, key).events_for_trace("t2", after=0, limit=2, max_pages=10)
            none = reader(base, key).events_for_trace("nope", after=0, limit=2, max_pages=10)
        self.assertEqual([e["id"] for e in EVENTS if e["traceId"] == "t2"], [e["id"] for e in got])
        self.assertEqual([], none)

    def test_a_failure_in_a_later_page_is_an_error_not_the_pages_before_it(self) -> None:
        def handler(method: str, path: str, query: dict[str, list[str]], headers: dict[str, str]) -> tuple[int, Any, dict[str, str]]:
            if query["after"][0] == "0":
                return 200, {"events": EVENTS[:2]}, {}
            return 503, {"error": {"code": "BUSY", "message": "m"}}, {}
        with stand_in(handler) as (base, seen), key_file() as key:
            with self.assertRaises(TusheReadError) as caught:
                reader(base, key).read_feed(after=0, limit=2, max_pages=5)
        self.assertEqual("UNAVAILABLE", caught.exception.kind)
        self.assertEqual(2, len(seen))


if __name__ == "__main__":
    unittest.main()
