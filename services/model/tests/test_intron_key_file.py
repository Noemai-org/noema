"""The Intron API key comes only from a file mount (A249; INV-29, AGENT-13).

`INTRON_API_KEY_FILE` names a 0600 file that only the model service reads, at call time. The key is never read from the environment itself, never logged, and never part of an error: a
refusal says what is wrong with the file in fixed words, not what is in it. SYNTHETIC key values (the recorded AGENT-12 exception of noema#412). The permission checks need a POSIX
file system and are skipped elsewhere; CI runs them on Linux.
"""

import os
import stat
from pathlib import Path

import pytest

from noema_model.providers.speech.intron_key import ENV_KEY_FILE, KeyFileError, key_reader

SYNTHETIC = "synthetic-synthetic-synthetic"
POSIX = pytest.mark.skipif(os.name == "nt", reason="file modes are POSIX")


def write(tmp_path: Path, content: str, mode: int = 0o600) -> Path:
    path = tmp_path / "intron_api_key"
    path.write_text(content)
    path.chmod(mode)
    return path


def test_the_variable_is_named_by_the_ruling() -> None:
    assert ENV_KEY_FILE == "INTRON_API_KEY_FILE"


@POSIX
def test_the_key_is_read_from_the_file_with_the_trailing_newline_removed(tmp_path: Path) -> None:
    path = write(tmp_path, SYNTHETIC + "\n")
    assert key_reader({ENV_KEY_FILE: str(path)})() == SYNTHETIC


@POSIX
def test_the_file_is_read_at_call_time_so_a_rotated_key_is_picked_up(tmp_path: Path) -> None:
    path = write(tmp_path, "first-synthetic-value")
    read = key_reader({ENV_KEY_FILE: str(path)})
    assert read() == "first-synthetic-value"
    write(tmp_path, "second-synthetic-value")
    assert read() == "second-synthetic-value"


def test_no_variable_is_a_refusal_never_a_default() -> None:
    with pytest.raises(KeyFileError):
        key_reader({})()


def test_the_key_itself_in_the_environment_is_not_accepted() -> None:
    with pytest.raises(KeyFileError):
        key_reader({"INTRON_API_KEY": SYNTHETIC})()


@POSIX
@pytest.mark.parametrize("mode", [0o640, 0o604, 0o644, 0o660, 0o666, 0o700])
def test_a_file_anyone_else_can_read_or_that_is_executable_is_refused(tmp_path: Path, mode: int) -> None:
    path = write(tmp_path, SYNTHETIC, mode)
    with pytest.raises(KeyFileError) as caught:
        key_reader({ENV_KEY_FILE: str(path)})()
    assert SYNTHETIC not in str(caught.value) and str(path) not in str(caught.value)


@POSIX
@pytest.mark.parametrize("mode", [0o600, 0o400])
def test_a_file_only_the_owner_can_read_is_accepted(tmp_path: Path, mode: int) -> None:
    path = write(tmp_path, SYNTHETIC, mode)
    assert stat.S_IMODE(path.stat().st_mode) == mode
    assert key_reader({ENV_KEY_FILE: str(path)})() == SYNTHETIC


@POSIX
def test_a_symbolic_link_is_refused(tmp_path: Path) -> None:
    target = write(tmp_path, SYNTHETIC)
    link = tmp_path / "link"
    link.symlink_to(target)
    with pytest.raises(KeyFileError):
        key_reader({ENV_KEY_FILE: str(link)})()


@POSIX
@pytest.mark.parametrize("content", ["", "\n", "   \n", "two\nlines", "has space", "a" * 513, "tab\tin"])
def test_an_empty_or_malformed_or_oversized_key_is_refused_without_echoing_it(tmp_path: Path, content: str) -> None:
    path = write(tmp_path, content)
    with pytest.raises(KeyFileError) as caught:
        key_reader({ENV_KEY_FILE: str(path)})()
    assert content.strip() == "" or content.strip() not in str(caught.value)


def test_a_missing_file_and_a_directory_are_refused(tmp_path: Path) -> None:
    for target in (tmp_path / "absent", tmp_path):
        with pytest.raises(KeyFileError):
            key_reader({ENV_KEY_FILE: str(target)})()


@POSIX
def test_a_named_pipe_is_refused_without_blocking_on_open(tmp_path: Path) -> None:  # Codex P2 on noema#689: open(2) on a FIFO with no writer blocks until one appears
    import threading

    fifo = tmp_path / "pipe"
    getattr(os, "mkfifo")(fifo, 0o600)  # POSIX only (the test is skipped elsewhere)
    outcome: list[BaseException | str] = []

    def attempt() -> None:
        try:
            outcome.append(key_reader({ENV_KEY_FILE: str(fifo)})())
        except KeyFileError as error:
            outcome.append(error)

    worker = threading.Thread(target=attempt, daemon=True)
    worker.start()
    worker.join(timeout=5.0)
    if worker.is_alive():  # unblock the stuck open so the test process can end, then fail
        descriptor = os.open(fifo, os.O_WRONLY)
        os.close(descriptor)
        worker.join(timeout=5.0)
        pytest.fail("opening a named pipe blocked")
    assert len(outcome) == 1 and isinstance(outcome[0], KeyFileError)


@POSIX
def test_a_file_owned_by_someone_else_than_the_effective_user_is_refused(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:  # SR on noema#689: the SEC-09 loader pattern
    path = write(tmp_path, SYNTHETIC)
    owner = os.geteuid()  # type: ignore[attr-defined,unused-ignore]
    monkeypatch.setattr(os, "geteuid", lambda: owner + 1)  # the reader runs as another user than the file's owner
    with pytest.raises(KeyFileError) as caught:
        key_reader({ENV_KEY_FILE: str(path)})()
    assert SYNTHETIC not in str(caught.value) and str(path) not in str(caught.value)


@POSIX
def test_a_file_owned_by_the_effective_user_is_accepted(tmp_path: Path) -> None:
    path = write(tmp_path, SYNTHETIC)
    assert key_reader({ENV_KEY_FILE: str(path)})() == SYNTHETIC


# ---- the credential Intron issued is 654 bytes: the ceiling was my own wrong assumption, not a rule of the vendor -----------------------------------------------------------------


def jwt_like(length: int) -> str:
    """A SYNTHETIC token of the shape of a long credential (base64url characters and dots) of exactly `length` characters."""
    alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
    body = "".join(alphabet[(i * 7 + 3) % len(alphabet)] for i in range(length))
    return body[:40] + "." + body[41:length - 40] + "." + body[length - 39:]


@pytest.mark.parametrize("length", [513, 654, 1024, 4096])
def test_a_key_longer_than_512_bytes_up_to_the_ceiling_is_accepted(tmp_path: Path, length: int) -> None:
    token = jwt_like(length)
    assert len(token) == length
    path = tmp_path / "intron_api_key"
    path.write_text(token + "\n")
    path.chmod(0o600)
    assert key_reader({ENV_KEY_FILE: str(path)})() == token


def test_a_file_beyond_the_ceiling_is_still_refused_without_echoing_it(tmp_path: Path) -> None:
    path = tmp_path / "intron_api_key"
    path.write_text("a" * 4097)
    path.chmod(0o600)
    with pytest.raises(KeyFileError) as caught:
        key_reader({ENV_KEY_FILE: str(path)})()
    assert "a" * 20 not in str(caught.value)
