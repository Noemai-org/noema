"""One-shot step that makes the migrate one-shot's password file (ENV-04; A521, A527; INV-29, AGENT-13).

cmd/migrate takes the migration login's password from a file (`--password-file`) and refuses one that is not mode 0400, owned by the user it runs as, and one line. The password is a Compose file
secret, which keeps the host user's ownership and mode, and Compose cannot change either. So this step runs once, as root, in a container with no network: it reads the file secret
`noema_migrator_password`, writes one file `password` into MIGRATOR_PASSWORD_OUT at mode 0400 owned by MIGRATOR_PASSWORD_UID:MIGRATOR_PASSWORD_GID (the core image's user, never root), and checks it by
reading it as that user. It is the secret's second holder, after postgres, and the migrate one-shot reads only the file it writes (A527). Any problem exits non-zero and leaves no file behind. A folder that
holds anything but this file is refused. It prints the file name only, never the password. SYNTHETIC local values."""
import os
import re
import stat
import sys

SRC = os.environ.get("MIGRATOR_PASSWORD_SRC", "/run/secrets")
OUT = os.environ.get("MIGRATOR_PASSWORD_OUT", "/out")
NAME = "password"
PASSWORD = re.compile(r"^[A-Za-z0-9._~-]{16,}$")  # one printable token: no space, no line break
WRITTEN: list[str] = []


def fail(message: str) -> None:
    print(f"migrator-password-init: {message}", file=sys.stderr)
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


def main() -> None:
    uid, gid = ident("MIGRATOR_PASSWORD_UID"), ident("MIGRATOR_PASSWORD_GID")
    try:
        with open(os.path.join(SRC, "noema_migrator_password"), "rb") as f:
            password = f.read().decode("utf-8", "replace").strip()
    except OSError:
        fail("noema_migrator_password cannot be read")
    if not PASSWORD.match(password):
        fail("noema_migrator_password is not at least 16 letters, digits or . _ ~ - (one printable token)")
    try:
        entries = os.listdir(OUT)
    except OSError:
        fail("the output folder cannot be read")
    for entry in entries:
        full = os.path.join(OUT, entry)
        if entry != NAME or os.path.islink(full) or not os.path.isfile(full):
            fail("the output folder holds an entry this step does not write")
    content = password + "\n"
    dst = os.path.join(OUT, NAME)
    try:
        os.unlink(dst)
    except FileNotFoundError:
        pass
    fd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
    WRITTEN.append(dst)
    try:
        os.write(fd, content.encode("utf-8"))
        os.fchown(fd, uid, gid)
        os.fchmod(fd, 0o400)
    finally:
        os.close(fd)
    st = os.stat(dst)
    if stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
        fail(f"{NAME} is not mode 0400 owned by {uid}")
    if read_as(dst, uid, gid) != content.encode("utf-8"):
        fail(f"{NAME} cannot be read as user {uid}")
    print(f"migrator-password-init: {NAME} written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
