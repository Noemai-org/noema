"""An exception from the default transport never carries an authorization-header value (INV-29, AGENT-13).

Reference: run 40577810935 (bench-live), first error line `ValueError: Invalid header value b'***'` (`http/client.py` `putheader`, reached through
`providers/llm/transport.py` `_post_blocking`). GitHub masked the secret in that log; the standard library's own exception text carried it.
The real transport and the real adapter run; the header value is invalid because the credential holds a newline, exactly as in that run.
The secret below is a SYNTHETIC test string, not a credential.
"""

import traceback

import pytest

from noema_model.providers.llm.base import AdapterSettings, ProviderError, ProviderFailure, ProviderRequest
from noema_model.providers.llm.openai_responses import OpenAIResponses
from noema_model.providers.llm.transport import UrllibTransport
from tests.support import run

SECRET = "sk-SYNTHETIC-not-a-credential-7f3a"
BAD_CREDENTIAL = SECRET + "\n"  # a newline makes the header value invalid, as the stored secret in run 40577810935 did
URL = "http://127.0.0.1:9/v1/responses"  # never connected to: the header check fails first


def _everything_the_exception_shows(exc: BaseException) -> str:
    return "\n".join([str(exc), repr(exc), *traceback.format_exception(exc)])


def test_the_transport_raises_a_provider_error_without_the_header_value() -> None:
    transport = UrllibTransport(allow_loopback_http=True)
    with pytest.raises(ProviderError) as caught:
        run(transport.post(URL, {"authorization": f"Bearer {BAD_CREDENTIAL}"}, b"{}", 2.0, 1024))
    exc = caught.value
    assert exc.kind is ProviderFailure.REJECTED
    assert SECRET not in _everything_the_exception_shows(exc)
    assert exc.__cause__ is None and exc.__context__ is None  # no chained exception that holds the value


def test_the_adapter_surfaces_the_same_failure_without_the_credential() -> None:
    settings = AdapterSettings(endpoint="http://127.0.0.1:9", credential=BAD_CREDENTIAL, api_version=None, timeout_ms=2000, max_response_bytes=1024)
    adapter = OpenAIResponses("openai", settings, UrllibTransport(allow_loopback_http=True))
    request = ProviderRequest("gpt-6-luna", "system", "user", "schema", {"type": "object"}, 100, 0.0)
    with pytest.raises(ProviderError) as caught:
        run(adapter.complete(request))
    assert SECRET not in _everything_the_exception_shows(caught.value)
    assert caught.value.__cause__ is None and caught.value.__context__ is None
