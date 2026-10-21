"""The evil provider's HTTP server (T4 item 9, ADV-08). Standard library only; it makes no outbound call.

OpenAI-compatible: POST /v1/chat/completions and /v1/responses (text, schema-forced JSON and tool calls), GET /v1/models,
plus the decision-model shape at POST /v1/decisions (decisions_format.py), GET /healthz. It ignores credentials (it does not
read the Authorization header) and logs the route, the status and sizes only, never a body or a header.

It exists only in the `bench` profile on `provider_egress`. No product image holds this code (DEC-2026-10-20 T4): the model
service reaches it through ordinary harness configuration, a different base URL, with no code path that knows it is evil.
"""
import hashlib
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable

from . import decisions_format as decisions
from .adversary import Config, adversarial_json, adversarial_text, tool_arguments

Json = Any

MAX_BODY_BYTES = 1_048_576
MAX_DRAIN_BYTES = 16_777_216
MODEL_ID = "evil-model-2026-10-21"
SYSTEM = "evil-provider-synthetic"


class ApiError(Exception):
    def __init__(self, status: int, code: str, message: str, kind: str = "invalid_request_error", param: str | None = None) -> None:
        super().__init__(message)
        self.status, self.code, self.kind, self.param = status, code, kind, param


def _tokens(text: str) -> int:
    return len(text.split())  # a deterministic stand-in for the vendor's count


def _digest(*parts: Json) -> str:
    return hashlib.sha256(json.dumps(parts, sort_keys=True).encode()).hexdigest()[:24]


def _schema_of(body: dict[str, Json]) -> Json | None:
    fmt = body.get("response_format")
    if isinstance(fmt, dict) and fmt.get("type") == "json_schema":
        inner = fmt.get("json_schema")
        if isinstance(inner, dict) and isinstance(inner.get("schema"), dict):
            return inner["schema"]
    text = body.get("text")
    if isinstance(text, dict) and isinstance(text.get("format"), dict) and text["format"].get("type") == "json_schema":
        schema = text["format"].get("schema")
        if isinstance(schema, dict):
            return schema
    if isinstance(fmt, dict) and fmt.get("type") == "json_object":
        return {"type": "object", "properties": {"result": {"type": "string"}}}
    return None


def _content(body: dict[str, Json], config: Config) -> str:
    schema = _schema_of(body)
    if schema is not None:
        return json.dumps(adversarial_json(schema, config), sort_keys=True, separators=(",", ":"))
    return adversarial_text(config)


def _usage_size(body: dict[str, Json]) -> int:
    return _tokens(json.dumps(body.get("messages", body.get("input", "")), sort_keys=True))


def chat_completion(body: dict[str, Json], config: Config) -> dict[str, Json]:
    model = body.get("model") if isinstance(body.get("model"), str) else MODEL_ID
    tools = [t for t in body.get("tools") or [] if isinstance(t, dict) and isinstance(t.get("function"), dict)]
    prompt_tokens = _usage_size(body)
    if tools and body.get("tool_choice") != "none":
        function = tools[0]["function"]
        arguments = tool_arguments(function.get("parameters") or {"type": "object"}, config)
        message: Json = {"role": "assistant", "content": None,
                         "tool_calls": [{"id": "call_evil_1", "type": "function", "function": {"name": function.get("name", "act"), "arguments": arguments}}]}
        finish, completion = "tool_calls", _tokens(arguments)
    else:
        content = _content(body, config)
        message, finish, completion = {"role": "assistant", "content": content}, "stop", _tokens(content)
    return {
        "id": f"chatcmpl-evil-{_digest(body, config.mode, config.seed)}",
        "object": "chat.completion",
        "created": 0,
        "model": model,
        "system_fingerprint": SYSTEM,
        "choices": [{"index": 0, "message": message, "finish_reason": finish}],
        "usage": {"prompt_tokens": prompt_tokens, "completion_tokens": completion, "total_tokens": prompt_tokens + completion},
    }


def response_object(body: dict[str, Json], config: Config) -> dict[str, Json]:
    model = body.get("model") if isinstance(body.get("model"), str) else MODEL_ID
    text = _content(body, config)
    inp, out = _usage_size(body), _tokens(text)
    ident = _digest(body, config.mode, config.seed)
    return {
        "id": f"resp_evil_{ident}",
        "object": "response",
        "created_at": 0,
        "status": "completed",
        "model": model,
        "output": [{"type": "message", "id": f"msg_evil_{ident}", "status": "completed", "role": "assistant",
                    "content": [{"type": "output_text", "text": text, "annotations": []}]}],
        "usage": {"input_tokens": inp, "output_tokens": out, "total_tokens": inp + out},
    }


class EvilServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, address: tuple[str, int], config: Config, script: dict[str, str], log: Callable[[dict[str, Json]], None]) -> None:
        super().__init__(address, Handler)
        self.config, self.script, self.log = config, script, log


class Handler(BaseHTTPRequestHandler):
    server: EvilServer
    protocol_version = "HTTP/1.1"

    def log_message(self, format: str, *args: Json) -> None:  # silence the default stderr log; we log JSON ourselves
        return

    def _send(self, status: int, payload: Json, request_bytes: int = 0) -> None:
        data = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
        self.server.log({"event": "request", "method": self.command, "path": self.path.split("?", 1)[0], "status": status,
                         "mode": self.server.config.mode, "request_bytes": request_bytes})

    def _error(self, err: ApiError, request_bytes: int = 0) -> None:
        self._send(err.status, {"error": {"message": str(err), "type": err.kind, "param": err.param, "code": err.code}}, request_bytes)

    def _body(self) -> tuple[dict[str, Json], int]:
        length_text = self.headers.get("Content-Length")
        if length_text is None or not length_text.isdigit():
            raise ApiError(411, "length_required", "Content-Length is required")
        length = int(length_text)
        if length > MAX_BODY_BYTES:
            # Read and drop a body up to the drain limit so the client sees the 413 instead of a broken pipe; a body
            # beyond it is never read and the connection is closed.
            if length <= MAX_DRAIN_BYTES:
                remaining = length
                while remaining > 0:
                    chunk = self.rfile.read(min(65536, remaining))
                    if not chunk:
                        break
                    remaining -= len(chunk)
            else:
                self.close_connection = True
            raise ApiError(413, "request_too_large", f"the body is larger than {MAX_BODY_BYTES} bytes")
        raw = self.rfile.read(length)

        def refuse(constant: str) -> Json:
            raise ApiError(400, "invalid_json", f"{constant} is not valid JSON")

        try:
            body = json.loads(raw, parse_constant=refuse)
        except ValueError as exc:
            if isinstance(exc, ApiError):
                raise
            raise ApiError(400, "invalid_json", "the body is not valid JSON") from exc
        if not isinstance(body, dict):
            raise ApiError(400, "invalid_request", "the body must be a JSON object")
        return body, length

    def do_GET(self) -> None:  # noqa: N802 - http.server API
        path = self.path.split("?", 1)[0]
        if path == "/healthz":
            self._send(200, {"status": "ok"})
        elif path == "/v1/models":
            self._send(200, {"object": "list", "data": [{"id": MODEL_ID, "object": "model", "created": 0, "owned_by": SYSTEM}]})
        elif path in ("/v1/chat/completions", "/v1/responses", "/v1/decisions"):
            self._error(ApiError(405, "method_not_allowed", f"use POST for {path}"))
        else:
            self._error(ApiError(404, "not_found", f"no route {path}"))

    def do_POST(self) -> None:  # noqa: N802 - http.server API
        path = self.path.split("?", 1)[0]
        size = 0
        try:
            if path in ("/healthz", "/v1/models"):
                raise ApiError(405, "method_not_allowed", f"use GET for {path}")
            if path not in ("/v1/chat/completions", "/v1/responses", "/v1/decisions"):
                raise ApiError(404, "not_found", f"no route {path}")
            body, size = self._body()
            if body.get("stream"):
                raise ApiError(400, "unsupported_stream", "this provider does not stream", param="stream")
            config = self.server.config
            if path == "/v1/chat/completions":
                self._send(200, chat_completion(body, config), size)
            elif path == "/v1/responses":
                self._send(200, response_object(body, config), size)
            else:
                try:
                    reply = decisions.answer(decisions.parse_request(body), config, self.server.script)
                except decisions.DecisionRequestError as exc:
                    raise ApiError(400, "invalid_decision_request", str(exc)) from exc
                self._send(200, reply, size)
        except ApiError as err:
            self._error(err, size)


def make_server(host: str, port: int, config: Config, script: dict[str, str], log: Callable[[dict[str, Json]], None]) -> EvilServer:
    return EvilServer((host, port), config, script, log)
