"""One-shot step that makes the per-tenant Trust login DSN files the core reads (A356; INV-29, AGENT-16, AGENT-13).

The core's NOEMA_TRUST_DSN_DIR holds one regular file per tenant, named by the tenant ID, each holding that tenant's `trust_rw__<tenant>` login as a DSN (the database binds the login to the
tenant, DEBT-04). A DSN holds a password, so the files are secrets, and Compose cannot give a file the owner or mode the core needs. So this step runs once, as root, in a container with no
network: it reads the file secret `noema_tenant_login_password` (the one password init.sh gave the tenant logins) and the tenant list, writes one file per tenant into TRUST_DSN_OUT at mode 0400
owned by TRUST_DSN_UID:TRUST_DSN_GID (the core's user, never root), and checks each by opening it as that user. Any problem exits non-zero and leaves no file behind, so the stack does not
start with a login the core cannot load. It prints only file names, never a password. SYNTHETIC local values."""
import os
import re
import stat
import sys

SRC = os.environ.get("TRUST_DSN_SRC", "/run/secrets")
OUT = os.environ.get("TRUST_DSN_OUT", "/out")
HOST, PORT, DATABASE = "postgres", "5432", "noema"
TENANT = re.compile(r"^ten_[0-9A-Za-z]{2,32}$")  # the core's tenantFileRe, and provision-tenants.sh's rule
PASSWORD = re.compile(r"^[A-Za-z0-9._~-]{16,}$")  # nothing that is a separator in a URL, so the DSN means what it says without quoting


def fail(message: str, written: tuple[str, ...] = ()) -> None:
    print(f"trust-dsn-init: {message}", file=sys.stderr)
    for name in written:
        try:
            os.unlink(os.path.join(OUT, name))
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
                os.setgroups([])  # no supplementary group of root's comes with the check
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
    uid, gid = ident("TRUST_DSN_UID"), ident("TRUST_DSN_GID")
    tenants = os.environ.get("TRUST_DSN_TENANTS", "").split()
    if not tenants:
        fail("TRUST_DSN_TENANTS names no tenant")
    for t in tenants:
        if not TENANT.match(t):
            fail("a tenant in TRUST_DSN_TENANTS is not a tenant ID (ten_ and 2 to 32 letters or digits)")
    if len(set(tenants)) != len(tenants):
        fail("a tenant is named twice in TRUST_DSN_TENANTS")
    try:
        with open(os.path.join(SRC, "noema_tenant_login_password"), "rb") as f:
            password = f.read().decode("utf-8", "replace").strip()
    except OSError:
        fail("noema_tenant_login_password cannot be read")
    if not PASSWORD.match(password):
        fail("noema_tenant_login_password is not at least 16 letters, digits or . _ ~ - (a DSN would need quoting)")
    written: list[str] = []
    for tenant in tenants:
        dsn = f"postgres://trust_rw__{tenant}:{password}@{HOST}:{PORT}/{DATABASE}"
        dst = os.path.join(OUT, tenant)
        try:
            os.unlink(dst)
        except FileNotFoundError:
            pass
        fd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        written.append(tenant)
        try:
            os.write(fd, (dsn + "\n").encode("utf-8"))
            os.fchown(fd, uid, gid)
            os.fchmod(fd, 0o400)
        finally:
            os.close(fd)
    for tenant in tenants:
        dst = os.path.join(OUT, tenant)
        st = os.stat(dst)
        if stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
            fail(f"{tenant} is not mode 0400 owned by {uid}", tuple(written))
        got = read_as(dst, uid, gid)
        if got is None or not got.decode("utf-8", "replace").startswith(f"postgres://trust_rw__{tenant}:"):
            fail(f"{tenant} cannot be read as user {uid}", tuple(written))
        print(f"trust-dsn-init: {tenant} written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
