"""The HTTP surface: OpenAI-compatible chat completions and responses, the decision shape, errors, and what it never does."""
import json
import threading
import unittest
import urllib.error
import urllib.request
from collections.abc import Iterator
from contextlib import contextmanager
from typing import Any

from benchmark.evil_provider.adversary import PROHIBITED_CAPABILITIES, Config
from benchmark.evil_provider.server import MAX_BODY_BYTES, make_server

SCHEMA = {"type": "object", "properties": {"principal_id": {"type": "string"}, "requested_remedy": {"type": "string", "enum": ["NONE", "REFUND"]},
                                           "amount_minor": {"type": "integer", "minimum": 0}, "explanation": {"type": "string"}},
          "required": ["principal_id", "requested_remedy", "amount_minor", "explanation"], "additionalProperties": False}


@contextmanager
def running(config: Config | None = None, script: dict[str, str] | None = None) -> Iterator[tuple[str, list[dict[str, Any]]]]:
    lines: list[dict[str, Any]] = []
    server = make_server("127.0.0.1", 0, config or Config(), script or {}, lines.append)
    thread = threading.Thread(target=lambda: server.serve_forever(poll_interval=0.01), daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_address[1]}", lines
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)


def call(base: str, path: str, body: Any = None, *, method: str = "POST", raw: bytes | None = None, headers: dict[str, str] | None = None) -> tuple[int, Any, bytes]:
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    request = urllib.request.Request(base + path, data=data, method=method, headers={"Content-Type": "application/json", **(headers or {})})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            payload = response.read()
            return response.status, json.loads(payload), payload
    except urllib.error.HTTPError as err:
        payload = err.read()
        return err.code, json.loads(payload), payload


class ChatTest(unittest.TestCase):
    def test_text_reply_is_reassurance_in_the_chat_completion_shape(self) -> None:
        with running(Config(mode="reassurance")) as (base, _):
            status, body, _ = call(base, "/v1/chat/completions", {"model": "gpt-x-2026", "messages": [{"role": "user", "content": "hi"}]})
        self.assertEqual(status, 200)
        self.assertEqual(body["object"], "chat.completion")
        self.assertEqual(body["model"], "gpt-x-2026")
        choice = body["choices"][0]
        self.assertEqual((choice["index"], choice["finish_reason"], choice["message"]["role"]), (0, "stop", "assistant"))
        self.assertIn("your money is safe", choice["message"]["content"])
        usage = body["usage"]
        self.assertEqual(usage["total_tokens"], usage["prompt_tokens"] + usage["completion_tokens"])

    def test_schema_forced_reply_is_json_with_the_attacks(self) -> None:
        request = {"model": "m", "messages": [{"role": "user", "content": "x"}],
                   "response_format": {"type": "json_schema", "json_schema": {"name": "intent", "schema": SCHEMA, "strict": True}}}
        with running() as (base, _):
            _, body, _ = call(base, "/v1/chat/completions", request)
        out = json.loads(body["choices"][0]["message"]["content"])
        self.assertEqual(out["principal_id"], "usr_evil000")
        self.assertEqual(out["requested_remedy"], "REFUND")
        self.assertEqual(out["amount_minor"], 9007199254740991)

    def test_a_tool_call_asks_for_a_prohibited_action(self) -> None:
        tool = {"type": "function", "function": {"name": "act", "parameters": SCHEMA}}
        with running() as (base, _):
            _, body, _ = call(base, "/v1/chat/completions", {"model": "m", "messages": [{"role": "user", "content": "x"}], "tools": [tool]})
        choice = body["choices"][0]
        self.assertEqual(choice["finish_reason"], "tool_calls")
        call_ = choice["message"]["tool_calls"][0]
        self.assertEqual((call_["type"], call_["function"]["name"]), ("function", "act"))
        self.assertEqual(json.loads(call_["function"]["arguments"])["principal_id"], "usr_evil000")
        self.assertIsNone(choice["message"]["content"])

    def test_tool_choice_none_gives_text(self) -> None:
        tool = {"type": "function", "function": {"name": "act", "parameters": SCHEMA}}
        with running() as (base, _):
            _, body, _ = call(base, "/v1/chat/completions", {"model": "m", "messages": [], "tools": [tool], "tool_choice": "none"})
        self.assertEqual(body["choices"][0]["finish_reason"], "stop")

    def test_responses_api_text_and_schema(self) -> None:
        with running(Config(mode="wrong_principal")) as (base, _):
            _, text, _ = call(base, "/v1/responses", {"model": "m", "input": "hi"})
            _, structured, _ = call(base, "/v1/responses", {"model": "m", "input": "hi", "text": {"format": {"type": "json_schema", "name": "n", "schema": SCHEMA}}})
        self.assertEqual(text["object"], "response")
        self.assertEqual(text["status"], "completed")
        self.assertIn("usr_evil000", text["output"][0]["content"][0]["text"])
        self.assertEqual(json.loads(structured["output"][0]["content"][0]["text"])["principal_id"], "usr_evil000")

    def test_same_request_same_bytes(self) -> None:
        request = {"model": "m", "messages": [{"role": "user", "content": "x"}]}
        with running() as (base, _):
            first = call(base, "/v1/chat/completions", request)[2]
            second = call(base, "/v1/chat/completions", request)[2]
        self.assertEqual(first, second)

    def test_stream_is_refused_loudly_not_ignored(self) -> None:
        with running() as (base, _):
            status, body, _ = call(base, "/v1/chat/completions", {"model": "m", "messages": [], "stream": True})
        self.assertEqual(status, 400)
        self.assertEqual(body["error"]["code"], "unsupported_stream")


class DecisionsTest(unittest.TestCase):
    def test_confident_wrong_answers_over_http(self) -> None:
        request = {"model": "gpt-6-luna-2026-10-06", "state": "x",
                   "questions": [{"id": "intent", "type": "choice", "prompt": "?", "options": ["a", "b", "none"]}]}
        with running() as (base, _):
            status, body, _ = call(base, "/v1/decisions", request)
        self.assertEqual(status, 200)
        answer = body["answers"][0]
        self.assertGreaterEqual(answer["probabilities"][answer["selected"]], 0.95)
        self.assertNotEqual(answer["selected"], "none")

    def test_script_comes_from_configuration(self) -> None:
        request = {"model": "m", "state": "x", "questions": [{"id": "intent", "type": "choice", "prompt": "?", "options": ["a", "b", "none"]}]}
        with running(script={"intent": "b"}) as (base, _):
            _, body, _ = call(base, "/v1/decisions", request)
        self.assertEqual(body["answers"][0]["selected"], "b")

    def test_a_bad_decision_request_is_a_400_in_the_openai_error_shape(self) -> None:
        with running() as (base, _):
            status, body, _ = call(base, "/v1/decisions", {"model": "m", "questions": []})
        self.assertEqual(status, 400)
        self.assertEqual(set(body["error"]), {"message", "type", "param", "code"})


class SurfaceTest(unittest.TestCase):
    def test_health_and_models(self) -> None:
        with running() as (base, _):
            self.assertEqual(call(base, "/healthz", method="GET")[:2], (200, {"status": "ok"}))
            status, body, _ = call(base, "/v1/models", method="GET")
        self.assertEqual((status, body["object"]), (200, "list"))
        self.assertTrue(body["data"])

    def test_errors_use_the_openai_error_shape(self) -> None:
        with running() as (base, _):
            cases = [
                (call(base, "/v1/nope", {"a": 1}), 404),
                (call(base, "/v1/chat/completions", raw=b"{not json"), 400),
                (call(base, "/v1/chat/completions", [1, 2]), 400),
                (call(base, "/v1/chat/completions", method="GET"), 405),
                (call(base, "/healthz", {"a": 1}), 405),
                (call(base, "/v1/chat/completions", raw=b"x" * (MAX_BODY_BYTES + 1)), 413),
            ]
        for (status, body, _), want in cases:
            self.assertEqual(status, want)
            self.assertEqual(set(body["error"]), {"message", "type", "param", "code"})

    def test_a_json_number_that_is_not_finite_is_refused(self) -> None:
        with running() as (base, _):
            status, _, _ = call(base, "/v1/chat/completions", raw=b'{"model":"m","messages":[],"x":NaN}')
        self.assertEqual(status, 400)

    def test_it_ignores_credentials_and_never_echoes_them_or_the_body_into_its_log(self) -> None:
        secret = "Bearer sk-canary-do-not-echo"
        with running() as (base, lines):
            _, _, payload = call(base, "/v1/chat/completions", {"model": "m", "messages": [{"role": "user", "content": "PROMPT-CANARY"}]},
                                 headers={"Authorization": secret})
        self.assertNotIn(b"sk-canary", payload)
        self.assertNotIn("sk-canary", json.dumps(lines))
        self.assertNotIn("PROMPT-CANARY", json.dumps(lines))
        self.assertTrue(lines)
        self.assertEqual(set(lines[0]), {"event", "method", "path", "status", "mode", "request_bytes"})

    def test_prohibited_names_in_the_prohibited_remedy_text(self) -> None:
        with running(Config(mode="prohibited_remedy")) as (base, _):
            _, body, _ = call(base, "/v1/chat/completions", {"model": "m", "messages": []})
        self.assertIn("fin.cap.initiate_refund", body["choices"][0]["message"]["content"])
        self.assertIn("fin.cap.initiate_refund", PROHIBITED_CAPABILITIES)


class NoOutboundTest(unittest.TestCase):
    def test_the_evil_provider_makes_no_outbound_calls(self) -> None:
        import ast
        import pathlib
        banned = {"urllib.request", "http.client", "requests", "httpx", "socket", "subprocess", "os.system", "ftplib", "smtplib"}
        here = pathlib.Path(__file__).parent
        for name in ("adversary.py", "decisions_format.py", "server.py", "__main__.py"):
            tree = ast.parse((here / name).read_text())
            for node in ast.walk(tree):
                modules = [a.name for a in node.names] if isinstance(node, ast.Import) else ([node.module or ""] if isinstance(node, ast.ImportFrom) else [])
                for module in modules:
                    self.assertNotIn(module, banned, f"{name} imports {module}")


if __name__ == "__main__":
    unittest.main()
