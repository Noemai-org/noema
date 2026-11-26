"""One-shot step that makes the core's model-client private key file (A667; SEC-09, REQ-SEC-009, INV-29).

The core reads its mTLS client key for the model service with the shared checked reader (keyfiles.ReadFile): a regular file owned by the process user, mode 0400, no link. Compose bind-mounts the
host file `secrets/tls/core-client.key` (mode 0600, the host user) and cannot change its owner or mode, so the core exited at start (the stack-check of 24 Nov). The reader is not changed. This step
runs once, as root, in a container with no network: it reads the key from CORE_TLS_SRC, checks that it is one PEM private key block of a sane size, writes it to CORE_TLS_OUT/core-client.key at
mode 0400 owned by CORE_TLS_UID:CORE_TLS_GID (the core's user, never root), and checks the result by reading it as that user. A directory that holds anything else is refused. Any problem exits
non-zero and leaves no file behind. It prints only the file name, never the key. SYNTHETIC local material."""
import os
import stat
import sys

SRC = os.environ.get("CORE_TLS_SRC", "/in/core-client.key")
OUT = os.environ.get("CORE_TLS_OUT", "/out")
NAME = "core-client.key"
MAX_BYTES = 16384  # the core's reader bound is the same order; a private key PEM is far smaller
BEGIN, END = b"-----BEGIN ", b"-----END "
DST = os.path.join(OUT, NAME)


def fail(message: str) -> None:
    print(f"core-tls-init: {message}", file=sys.stderr)
    try:
        os.unlink(DST)
    except FileNotFoundError:
        pass
    sys.exit(1)


def ident(name: str) -> int:
    raw = os.environ.get(name, "")
    if not raw.isdigit() or int(raw) == 0:
        fail(f"{name} must be the numeric id of a user that is not root")
    return int(raw)


def read_source() -> bytes:
    try:
        st = os.lstat(SRC)
    except OSError:
        fail("the key cannot be read")
    if not stat.S_ISREG(st.st_mode):  # lstat: a link is not a regular file
        fail("the key is not a regular file")
    if st.st_size == 0 or st.st_size > MAX_BYTES:
        fail("the key is empty or larger than the bound")
    try:
        with open(SRC, "rb") as f:
            data = f.read(MAX_BYTES + 1)
    except OSError:
        fail("the key cannot be read")
    lines = data.strip().splitlines()
    ok = len(lines) >= 3 and lines[0].startswith(BEGIN) and lines[0].rstrip().endswith(b"PRIVATE KEY-----") and lines[-1].startswith(END) and lines[-1].rstrip().endswith(b"PRIVATE KEY-----")
    if not ok or data.count(BEGIN) != 1:
        fail("the file is not one PEM private key block")
    return data


def main() -> None:
    uid, gid = ident("CORE_TLS_UID"), ident("CORE_TLS_GID")
    data = read_source()
    try:
        others = [n for n in os.listdir(OUT) if n != NAME]
    except OSError:
        fail("the output directory cannot be read")
    if others:
        fail("the output directory holds a file other than the key this step writes")
    try:
        os.unlink(DST)
    except FileNotFoundError:
        pass
    fd = os.open(DST, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o400)
    try:
        os.write(fd, data)
        os.fchown(fd, uid, gid)
        os.fchmod(fd, 0o400)
    finally:
        os.close(fd)
    st = os.lstat(DST)
    if not stat.S_ISREG(st.st_mode) or stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
        fail(f"{NAME} is not a regular file of mode 0400 owned by {uid}")
    pid = os.fork()  # check as the user that will read it, in a child so this process keeps its own identity
    if pid == 0:
        code = 1
        try:
            if os.geteuid() == 0:
                os.setgroups([])
                os.setgid(gid)
                os.setuid(uid)
            with open(DST, "rb") as f:
                code = 0 if f.read() == data else 1
        except OSError:
            code = 1
        os._exit(code)
    _, status = os.waitpid(pid, 0)
    if os.waitstatus_to_exitcode(status) != 0:
        fail(f"{NAME} cannot be read as user {uid}")
    print(f"core-tls-init: {NAME} written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
