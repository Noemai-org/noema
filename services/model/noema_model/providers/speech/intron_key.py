"""Where the Intron API key comes from (A249; INV-29, AGENT-13).

Only from a file: `INTRON_API_KEY_FILE` names a regular file that only its owner can read (no group or other bits, not executable), read by the model service only and at call time, so a
rotated key is picked up without a restart. The key is never read from the environment itself, never logged, and never part of an error; a refusal names what is wrong with the file in
fixed words and says nothing of its content or its path. There is no default: no variable is a refusal.
"""

import os
import stat
from collections.abc import Callable, Mapping

ENV_KEY_FILE = "INTRON_API_KEY_FILE"
MAX_KEY_BYTES = 512  # a ceiling on what is read, so a wrong file (a log, a binary) is refused instead of loaded; Intron's keys are far shorter
_FORBIDDEN_MODE = stat.S_IRWXG | stat.S_IRWXO | stat.S_IXUSR


class KeyFileError(Exception):
    """The key file is missing or unusable. The message is fixed text and never carries the key or the path."""


def key_reader(environ: Mapping[str, str]) -> Callable[[], str]:
    """A function that reads the key from the file the variable names, each time it is called. The variable is read once, here."""
    path = environ.get(ENV_KEY_FILE)

    def read() -> str:
        if not path:
            raise KeyFileError("the Intron key file is not configured")
        try:
            descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NONBLOCK", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0))  # O_NONBLOCK: opening a named pipe must not wait for a writer (Codex P2 on #689); the type is checked next
        except OSError:
            raise KeyFileError("the Intron key file cannot be opened") from None
        try:
            info = os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode):
                raise KeyFileError("the Intron key file is not a regular file")
            if os.name != "nt" and stat.S_IMODE(info.st_mode) & _FORBIDDEN_MODE:
                raise KeyFileError("the Intron key file must be readable by its owner only")
            if os.name != "nt" and info.st_uid != getattr(os, "geteuid")():
                raise KeyFileError("the Intron key file must be owned by the user that reads it")  # the SEC-09 loader pattern (SR on #689)
            raw = os.read(descriptor, MAX_KEY_BYTES + 1)
        except OSError:
            raise KeyFileError("the Intron key file cannot be read") from None
        finally:
            os.close(descriptor)
        if len(raw) > MAX_KEY_BYTES:
            raise KeyFileError("the Intron key file is too large to be a key")
        try:
            key = raw.decode("ascii").removesuffix("\n")
        except UnicodeDecodeError:
            raise KeyFileError("the Intron key file is not a key") from None
        if not key or not key.isprintable() or any(c.isspace() for c in key):
            raise KeyFileError("the Intron key file is not a key")
        return key

    return read
