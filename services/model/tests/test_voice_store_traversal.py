"""The audio store opens every path component through a verified directory descriptor and reads until it has the whole file (VOI-16, REQ-VOI-004, AGENT-13; Codex REQUEST_CHANGES on noema#824 at 043bfd68).

`O_NOFOLLOW` covers only the last component of a path, so a symbolic link in place of a speaker folder (or of the store itself after it was checked) was followed on a read, and a single `os.read` may
return fewer bytes than the file holds. These are adversarial tests with real links and real files in a temporary directory; the short read is the real `os.read` limited to a few bytes a call.
They need POSIX file modes and run on Linux CI.
"""

import os
from datetime import UTC, datetime
from pathlib import Path

import pytest

from noema_model.voice.baseline import AudioStore, StoreError

POSIX = pytest.mark.skipif(os.name == "nt", reason="file modes and directory descriptors are POSIX")
ANALYSED = datetime(2026, 11, 20, 9, 30, tzinfo=UTC)


def now() -> datetime:
    return datetime(2026, 11, 21, 12, 0, tzinfo=UTC)


def private_dir(path: Path) -> Path:
    path.mkdir(mode=0o700)
    path.chmod(0o700)
    return path


def outside_with_a_recording(tmp_path: Path) -> Path:
    """A directory of someone else's with a file in it, standing in for any place outside the store."""
    outside = private_dir(tmp_path / "outside")
    (outside / "u1.pcm").write_bytes(b"SECRET-RAW-AUDIO")
    return outside


@POSIX
def test_a_symbolic_link_in_place_of_a_speaker_folder_is_not_followed_on_a_read(tmp_path: Path) -> None:
    root = private_dir(tmp_path / "store")
    (root / "spk-01").symlink_to(outside_with_a_recording(tmp_path), target_is_directory=True)
    with pytest.raises(StoreError) as caught:
        AudioStore(root, now).read("spk-01", "u1.pcm")
    assert "SECRET" not in str(caught.value)


@POSIX
def test_a_store_swapped_for_a_link_after_it_was_checked_is_refused_on_a_read_and_on_a_write(tmp_path: Path) -> None:
    root = private_dir(tmp_path / "store")
    store = AudioStore(root, now)
    elsewhere = private_dir(tmp_path / "elsewhere")
    spk = private_dir(elsewhere / "spk-01")
    (spk / "u1.pcm").write_bytes(b"SECRET-RAW-AUDIO")
    root.rename(tmp_path / "store-moved")
    root.symlink_to(elsewhere, target_is_directory=True)  # the path now leads somewhere else
    with pytest.raises(StoreError):
        store.read("spk-01", "u1.pcm")
    with pytest.raises(StoreError):
        store.write("spk-01", "new.pcm", b"x", ANALYSED)
    assert sorted(p.name for p in spk.iterdir()) == ["u1.pcm"]  # nothing was written through the link


@POSIX
def test_a_symbolic_link_in_place_of_a_speaker_folder_is_not_written_through(tmp_path: Path) -> None:
    root = private_dir(tmp_path / "store")
    outside = private_dir(tmp_path / "outside")
    (root / "spk-01").symlink_to(outside, target_is_directory=True)
    with pytest.raises(StoreError):
        AudioStore(root, now).write("spk-01", "u1.pcm", b"x", ANALYSED)
    assert list(outside.iterdir()) == []


@POSIX
def test_a_speaker_folder_that_is_a_plain_file_is_refused_on_a_read(tmp_path: Path) -> None:
    root = private_dir(tmp_path / "store")
    (root / "spk-01").write_bytes(b"not a folder")
    with pytest.raises(StoreError):
        AudioStore(root, now).read("spk-01", "u1.pcm")


@POSIX
def test_a_file_the_system_returns_in_pieces_is_read_whole(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    root = private_dir(tmp_path / "store")
    store = AudioStore(root, now)
    data = bytes(range(256)) * 4
    store.write("spk-01", "u1.pcm", data, ANALYSED)
    real_read = os.read
    calls: list[int] = []

    def short_read(descriptor: int, count: int) -> bytes:
        calls.append(count)
        return real_read(descriptor, min(count, 7))  # a short read, as a pipe or a network file system may give

    monkeypatch.setattr(os, "read", short_read)
    assert store.read("spk-01", "u1.pcm") == data
    assert len(calls) > 1


@POSIX
def test_a_file_that_ends_before_its_declared_size_is_refused_not_returned_short(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    root = private_dir(tmp_path / "store")
    store = AudioStore(root, now)
    store.write("spk-01", "u1.pcm", b"x" * 100, ANALYSED)
    real_read = os.read
    served = {"n": 0}

    def truncating_read(descriptor: int, count: int) -> bytes:
        served["n"] += 1
        return real_read(descriptor, min(count, 40)) if served["n"] == 1 else b""  # the file shrank after its size was read

    monkeypatch.setattr(os, "read", truncating_read)
    with pytest.raises(StoreError):
        store.read("spk-01", "u1.pcm")
