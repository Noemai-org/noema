"""One-shot step that makes the core's key files (A506, A469; INV-29, AGENT-16, AGENT-13).

The core loads three things from three directories: the orchestrator's signing key (`core-workflows.ed25519.1.key`), the gateway's (`core-edge.ed25519.1.key`) and the input hash key (one file). Each
is a value of 64 lower-case hexadecimal characters with at most one trailing newline, mode exactly 0400, owned by the core's user (A506). Compose cannot give a file secret that mode or owner. So this
step runs once, as root, in a container with no network: for each `<secret name>=<absolute path>` of CORE_KEYS_FILES it reads the file secret from CORE_KEYS_SRC, writes the value to the path at mode
0400 owned by CORE_KEYS_UID:CORE_KEYS_GID (the core's user, never root), and checks the file by reading it as that user. Every value is checked before anything is written; the values must be
different from one another; a directory that holds anything but the files this step writes is refused (the loader reads every key file in its directory). Any problem exits non-zero and leaves no
file behind. It prints only file names, never a value. SYNTHETIC local values."""
import os
import re
import stat
import sys

SRC = os.environ.get("CORE_KEYS_SRC", "/run/secrets")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
NAME = re.compile(r"^[a-z][a-z0-9_]{2,63}$")
WRITTEN: list[str] = []


def fail(message: str) -> None:
    print(f"core-keys-init: {message}", file=sys.stderr)
    for path in WRITTEN:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
    sys.exit(1)


def ident(name: str) -> int:
    raw = os.environ.get(name, "")
    if not raw.isdigit() or int(raw) == 0:
        fail(f"{name} must be the numeric id of a user that is not root")
    return int(raw)


def read_as(path: str, uid: int, gid: int) -> bytes | None:
    """The file's content read by the target user in a child, so this process keeps its own identity; None when that user cannot read it."""
    r, w = os.pipe()
    pid = os.fork()
    if pid == 0:
        code = 1
        try:
            os.close(r)
            if os.geteuid() == 0:
                os.setgroups([])
                os.setgid(gid)
                os.setuid(uid)
            with open(path, "rb") as f:
                os.write(w, f.read())
            code = 0
        except OSError:
            code = 1
        os._exit(code)
    os.close(w)
    data = b""
    while chunk := os.read(r, 65536):
        data += chunk
    os.close(r)
    _, status = os.waitpid(pid, 0)
    return data if os.waitstatus_to_exitcode(status) == 0 else None


def spec() -> list[tuple[str, str]]:
    found: list[tuple[str, str]] = []
    for item in os.environ.get("CORE_KEYS_FILES", "").split():
        name, _, path = item.partition("=")
        if not NAME.match(name) or not path.startswith("/") or ".." in path.split("/") or path.endswith("/"):
            fail("an entry of CORE_KEYS_FILES is not <secret name>=<absolute file path>")
        found.append((name, path))
    if not found or len({n for n, _ in found}) != len(found) or len({p for _, p in found}) != len(found):
        fail("CORE_KEYS_FILES must name at least one file and each secret and each path once")
    return found


def main() -> None:
    uid, gid = ident("CORE_KEYS_UID"), ident("CORE_KEYS_GID")
    files = spec()
    values: dict[str, bytes] = {}
    for name, _ in files:
        try:
            with open(os.path.join(SRC, name), "rb") as f:
                values[name] = f.read().strip()
        except OSError:
            fail("a secret of CORE_KEYS_FILES cannot be read")
        if not HEX64.match(values[name].decode("ascii", "replace")):
            fail("a secret of CORE_KEYS_FILES is not 64 lower-case hexadecimal characters")
    if len(set(values.values())) != len(values):
        fail("two secrets of CORE_KEYS_FILES hold the same value: each key must be its own")
    expected: dict[str, set[str]] = {}
    for _, path in files:
        expected.setdefault(os.path.dirname(path), set()).add(os.path.basename(path))
    for directory, names in expected.items():
        try:
            entries = os.listdir(directory)
        except OSError:
            fail("a key directory cannot be read")
        for entry in entries:
            full = os.path.join(directory, entry)
            if entry not in names or os.path.islink(full) or not os.path.isfile(full):
                fail("a key directory holds an entry this step does not write")
    for name, path in files:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        WRITTEN.append(path)
        try:
            os.write(fd, values[name] + b"\n")
            os.fchown(fd, uid, gid)
            os.fchmod(fd, 0o400)
        finally:
            os.close(fd)
    for name, path in files:
        st = os.stat(path)
        if stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
            fail(f"{os.path.basename(path)} is not mode 0400 owned by {uid}")
        if read_as(path, uid, gid) != values[name] + b"\n":
            fail(f"{os.path.basename(path)} cannot be read as user {uid}")
        print(f"core-keys-init: {os.path.basename(path)} written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
