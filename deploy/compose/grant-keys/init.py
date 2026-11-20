"""One-shot step that makes the Decision Grant key files the core can load (SEC-09, INV-29; Codex and SR on noema#700).

Compose cannot give a file secret a mode or an owner (it bind-mounts the host file), and the grant-key loader refuses a key file that is not mode 0400 and owned by the process user. So
this step runs once, as root, in a container with no network: it reads the two key secrets from GRANT_KEYS_SRC (grant.hmac.1.key and grant.hmac.2.key, 64 hexadecimal characters each,
different from each other), writes them to GRANT_KEYS_OUT at mode 0400 owned by GRANT_KEYS_UID:GRANT_KEYS_GID (the core's user, never root), and then checks the result by opening each
file as that user. Any problem exits non-zero and leaves no key file behind, so the stack does not start with keys the core cannot load. The loader names the version in the file name; the
highest version signs. It prints only file names, never a key. SYNTHETIC local values."""
import os
import re
import stat
import sys

VERSIONS = (1, 2)
SRC = os.environ.get("GRANT_KEYS_SRC", "/run/secrets")
OUT = os.environ.get("GRANT_KEYS_OUT", "/out")
HEX64 = re.compile(r"^[0-9a-f]{64}$")


def fail(message: str) -> None:
    print(f"grant-keys-init: {message}", file=sys.stderr)
    for v in VERSIONS:
        try:
            os.unlink(os.path.join(OUT, f"grant.hmac.{v}.key"))
        except FileNotFoundError:
            pass
    sys.exit(1)


def ident(name: str) -> int:
    raw = os.environ.get(name, "")
    if not raw.isdigit() or int(raw) == 0:
        fail(f"{name} must be the numeric id of a user that is not root")
    return int(raw)


def main() -> None:
    uid, gid = ident("GRANT_KEYS_UID"), ident("GRANT_KEYS_GID")
    keys: dict[int, bytes] = {}
    for v in VERSIONS:
        try:
            with open(os.path.join(SRC, f"grant.hmac.{v}.key"), "rb") as f:
                keys[v] = f.read().strip()
        except OSError:
            fail(f"grant.hmac.{v}.key cannot be read")
        if not HEX64.match(keys[v].decode("ascii", "replace")):
            fail(f"grant.hmac.{v}.key is not 64 lowercase hexadecimal characters")
    if len(set(keys.values())) != len(keys):
        fail("the two grant keys are the same; a rotation needs two different keys")
    for v, value in keys.items():
        dst = os.path.join(OUT, f"grant.hmac.{v}.key")
        try:
            os.unlink(dst)
        except FileNotFoundError:
            pass
        fd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        try:
            os.write(fd, value + b"\n")
            os.fchown(fd, uid, gid)
            os.fchmod(fd, 0o400)
        finally:
            os.close(fd)
    for v, value in keys.items():  # check as the user that will read them, in a child so this process keeps its own identity
        dst = os.path.join(OUT, f"grant.hmac.{v}.key")
        st = os.stat(dst)
        if stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
            fail(f"grant.hmac.{v}.key is not mode 0400 owned by {uid}")
        pid = os.fork()
        if pid == 0:
            code = 1
            try:
                if os.geteuid() == 0:
                    os.setgid(gid)
                    os.setuid(uid)
                with open(dst, "rb") as f:
                    code = 0 if f.read().strip() == value else 1
            except OSError:
                code = 1
            os._exit(code)
        _, status = os.waitpid(pid, 0)
        if os.waitstatus_to_exitcode(status) != 0:
            fail(f"grant.hmac.{v}.key cannot be read as user {uid}")
        print(f"grant-keys-init: grant.hmac.{v}.key written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
