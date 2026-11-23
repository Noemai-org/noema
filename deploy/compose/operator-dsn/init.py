"""One-shot step that makes the operator DSN file the tenant-init step reads (ENV-04; A445, A489, A527; INV-29, AGENT-13).

cmd/tenantinit registers the core-trust public key with `key_register`, which only its owner and the superuser may call (migration 0013). The login is the superuser's: the migration login's password
is held by the migrate one-shot and postgres alone (A527). A DSN holds a password and Compose cannot give a file the owner or mode the loader needs, so this step runs once, as root, in a container
with no network: it reads the file secret `postgres_superuser_password`, writes one file `dsn` into OPERATOR_DSN_OUT at mode 0400 owned by OPERATOR_DSN_UID:OPERATOR_DSN_GID (the core image's user,
never root), and checks it by reading it as that user. Any problem exits non-zero and leaves no file behind, so the stack does not start with a login the next step cannot load. A folder that holds
anything but this file is refused. It prints file names only, never the password. It also writes `pgpass` (libpq's password file, one line) for the create-tenants one-shot, which passes no password and reads PGPASSFILE. SYNTHETIC local values."""
import os
import re
import stat
import sys

SRC = os.environ.get("OPERATOR_DSN_SRC", "/run/secrets")
OUT = os.environ.get("OPERATOR_DSN_OUT", "/out")
NAME = "dsn"
PGPASS = "pgpass"  # libpq's password file for the psql one-shot: PGPASSFILE names it; one line, host:port:database:login:password
NAMES = (NAME, PGPASS)
HOST, PORT, DATABASE, LOGIN = "postgres", "5432", "noema", "postgres"
PASSWORD = re.compile(r"^[A-Za-z0-9._~-]{16,}$")  # nothing that is a separator in a URL, so the DSN means what it says without quoting
WRITTEN: list[str] = []


def fail(message: str) -> None:
    print(f"operator-dsn-init: {message}", file=sys.stderr)
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
    uid, gid = ident("OPERATOR_DSN_UID"), ident("OPERATOR_DSN_GID")
    try:
        with open(os.path.join(SRC, "postgres_superuser_password"), "rb") as f:
            password = f.read().decode("utf-8", "replace").strip()
    except OSError:
        fail("postgres_superuser_password cannot be read")
    if not PASSWORD.match(password):
        fail("postgres_superuser_password is not at least 16 letters, digits or . _ ~ - (a DSN would need quoting)")
    try:
        entries = os.listdir(OUT)
    except OSError:
        fail("the output folder cannot be read")
    for entry in entries:
        full = os.path.join(OUT, entry)
        if entry not in NAMES or os.path.islink(full) or not os.path.isfile(full):
            fail("the output folder holds an entry this step does not write")
    dsn = f"postgres://{LOGIN}:{password}@{HOST}:{PORT}/{DATABASE}\n"
    pgpass_line = f"{HOST}:{PORT}:{DATABASE}:{LOGIN}:{password}\n"
    for name, data in ((NAME, dsn), (PGPASS, pgpass_line)):
        dst = os.path.join(OUT, name)
        try:
            os.unlink(dst)
        except FileNotFoundError:
            pass
        fd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        WRITTEN.append(dst)
        try:
            os.write(fd, data.encode("utf-8"))
            os.fchown(fd, uid, gid)
            os.fchmod(fd, 0o400)
        finally:
            os.close(fd)
        st = os.stat(dst)
        if stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
            fail(f"{name} is not mode 0400 owned by {uid}")
        if read_as(dst, uid, gid) != data.encode("utf-8"):
            fail(f"{name} cannot be read as user {uid}")
        print(f"operator-dsn-init: {name} written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
