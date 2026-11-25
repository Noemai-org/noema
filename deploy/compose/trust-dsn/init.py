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
EMITTERS = os.environ.get("TRUST_DSN_EMITTERS", "/run/emitter_logins")
# A506 (interim, from the code): the core loads four DSN directories, one file per tenant named by the tenant id. TRUST_DSN_JOBS is "<group>:<directory> ..."; without it there is one job, the
# Trust login into TRUST_DSN_OUT. The first three groups use the shared tenant login password; `edge` is the per-login emitter secret of the core-edge login.
GROUPS = ("trust_rw", "orchestrator_rw", "record_read", "edge", "execution_rw", "outcome_rw")
WRITTEN: list[str] = []  # every file made by any job: a failure removes all of them, so no half-written set is left
HOST, PORT, DATABASE = "postgres", "5432", "noema"
TENANT = re.compile(r"^ten_[0-9A-Za-z]{2,32}$")  # the core's tenantFileRe, and provision-tenants.sh's rule
PASSWORD = re.compile(r"^[A-Za-z0-9._~-]{16,}$")  # nothing that is a separator in a URL, so the DSN means what it says without quoting


def fail(message: str) -> None:
    print(f"trust-dsn-init: {message}", file=sys.stderr)
    for path in WRITTEN:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
    sys.exit(1)


def login_of(group: str, tenant: str) -> str:
    return f"record_emitter__{tenant}__core-edge" if group == "edge" else f"{group}__{tenant}"


def jobs() -> list[tuple[str, str]]:
    spec = os.environ.get("TRUST_DSN_JOBS")
    if spec is None:
        return [("trust_rw", OUT)]
    found: list[tuple[str, str]] = []
    for item in spec.split():
        group, _, out = item.partition(":")
        if group not in GROUPS or not out.startswith("/") or ".." in out.split("/"):
            fail("a job of TRUST_DSN_JOBS is not <group>:<absolute directory> of a known group")
        found.append((group, out))
    if not found or len({g for g, _ in found}) != len(found) or len({o for _, o in found}) != len(found):
        fail("TRUST_DSN_JOBS must name each group and each directory once, and at least one job")
    return found


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
    plan = jobs()  # every job is checked before any file is written
    passwords: dict[tuple[str, str], str] = {}
    shared = ""
    if any(group != "edge" for group, _ in plan):
        try:
            with open(os.path.join(SRC, "noema_tenant_login_password"), "rb") as f:
                shared = f.read().decode("utf-8", "replace").strip()
        except OSError:
            fail("noema_tenant_login_password cannot be read")
        if not PASSWORD.match(shared):
            fail("noema_tenant_login_password is not at least 16 letters, digits or . _ ~ - (a DSN would need quoting)")
    for group, _ in plan:
        for tenant in tenants:
            if group != "edge":
                passwords[(group, tenant)] = shared
                continue
            try:
                with open(os.path.join(EMITTERS, f"{tenant}__core-edge"), "rb") as f:
                    secret = f.read().decode("utf-8", "replace").strip()
            except OSError:
                fail("a core-edge login secret cannot be read")
            if not PASSWORD.match(secret):
                fail("a core-edge login secret is not at least 16 letters, digits or . _ ~ - (a DSN would need quoting)")
            passwords[(group, tenant)] = secret
    for group, out in plan:
        run_job(group, out, tenants, passwords, uid, gid)


def run_job(group: str, out: str, tenants: list[str], passwords: dict[tuple[str, str], str], uid: int, gid: int) -> None:
    # The volume is persistent and the core builds a pool for every file in it: a tenant dropped from the list must not keep its DSN file, and anything that is not a tenant file stops the step (C1's P3).
    for name in os.listdir(out):
        path = os.path.join(out, name)
        if not TENANT.match(name) or os.path.islink(path) or not os.path.isfile(path):
            fail("the DSN volume holds an entry that is not a tenant file")
        if name not in tenants:
            os.unlink(path)
    for tenant in tenants:
        dsn = f"postgres://{login_of(group, tenant)}:{passwords[(group, tenant)]}@{HOST}:{PORT}/{DATABASE}"
        dst = os.path.join(out, tenant)
        try:
            os.unlink(dst)
        except FileNotFoundError:
            pass
        fd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
        WRITTEN.append(dst)
        try:
            os.write(fd, (dsn + "\n").encode("utf-8"))
            os.fchown(fd, uid, gid)
            os.fchmod(fd, 0o400)
        finally:
            os.close(fd)
    for tenant in tenants:
        dst = os.path.join(out, tenant)
        st = os.stat(dst)
        if stat.S_IMODE(st.st_mode) != 0o400 or st.st_uid != uid:
            fail(f"{tenant} is not mode 0400 owned by {uid}")
        got = read_as(dst, uid, gid)
        if got is None or not got.decode("utf-8", "replace").startswith(f"postgres://{login_of(group, tenant)}:"):
            fail(f"{tenant} cannot be read as user {uid}")
        print(f"trust-dsn-init: {group}: {tenant} written, mode 0400, owner {uid}, readable by that user")


if __name__ == "__main__":
    main()
