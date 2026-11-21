"""The core image carries the operator commands, and the SEC-09 rotation drill runs in it as a CI job (SR's answer on noema#692; A445; REQ-SEC-005, INV-03, INV-25, INV-29).

`grantdrill` does not need the stack: it makes synthetic keys, loads them with the loader the core uses and runs the rotation drill on simulated time, so the job runs it in the core image with a tmpfs work
directory, no network, no secret and no capability. `policypublish` is built into the same image because the policy-publish one-shot of the bench stack (A445) runs it from there with its own entrypoint.
The image's own entrypoint stays the core. These tests read the real Dockerfile and the real workflow."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DOCKERFILE = (ROOT / "services" / "core" / "Dockerfile").read_text(encoding="utf-8")
WORKFLOW = (ROOT / ".github" / "workflows" / "compose.yml").read_text(encoding="utf-8")
COMMANDS = ("core", "grantdrill", "policypublish")


def job(name: str) -> str:
    m = re.search(r"(?ms)^  " + re.escape(name) + r":\n(.*?)(?=^  [a-z][a-z-]*:\n|\Z)", WORKFLOW)
    assert m, f"no job {name}"
    return m.group(1)


class CoreImageCommands(unittest.TestCase):
    def test_the_image_builds_and_carries_the_three_commands(self) -> None:
        for name in COMMANDS:
            self.assertIn("./cmd/" + name, DOCKERFILE, f"the build does not name ./cmd/{name}")
            self.assertRegex(DOCKERFILE, r"COPY --from=build [^\n]*/out/" + name + r"[^\n]* /usr/local/bin/", f"/usr/local/bin/{name} is not copied")

    def test_the_image_still_starts_the_core_as_the_nonroot_user_from_the_pinned_bases(self) -> None:
        self.assertRegex(DOCKERFILE, r'(?m)^ENTRYPOINT \["/usr/local/bin/core"\]$')
        self.assertRegex(DOCKERFILE, r"(?m)^USER nonroot:nonroot$")
        self.assertEqual(len(re.findall(r"(?m)^FROM \S+@sha256:[0-9a-f]{64}", DOCKERFILE)), 2, "both bases are pinned by digest")
        self.assertNotRegex(DOCKERFILE, r"(?m)^ENTRYPOINT .*(grantdrill|policypublish)")

    def test_the_build_is_static_and_reproducible_as_before(self) -> None:
        self.assertIn("CGO_ENABLED=0", DOCKERFILE)
        self.assertIn("-trimpath", DOCKERFILE)


class GrantDrillJob(unittest.TestCase):
    def test_the_job_builds_the_core_image_and_runs_the_drill_in_it_with_nothing_but_a_tmpfs(self) -> None:
        body = job("grantdrill")
        self.assertIn("persist-credentials: false", body)
        self.assertNotIn("secrets.", body)
        self.assertNotIn("environment:", body)
        self.assertIn("docker build", body)
        self.assertIn("services/core", body)
        run = re.search(r"docker run([^\n]*\\\n)*[^\n]*grantdrill[^\n]*", body)
        assert run, "no docker run of grantdrill"
        text = re.sub(r"\\\n\s*", " ", run.group(0))
        for flag in ("--rm", "--network none", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges:true", "--entrypoint /usr/local/bin/grantdrill", "--work /work"):
            self.assertIn(flag, text, flag)
        tmpfs = re.search(r"--tmpfs (\S+)", text)
        assert tmpfs
        for option in ("/work:", "rw", "noexec", "nosuid", "nodev", "mode=0700", "uid=65532", "gid=65532"):
            self.assertIn(option, tmpfs.group(1), option)
        self.assertNotRegex(text, r"(?<!-)-v |--volume|--mount|--env|-e |--privileged|--network (?!none)")

    def test_the_drill_is_a_job_of_its_own_and_its_result_is_the_commands_exit_code(self) -> None:
        body = job("grantdrill")
        self.assertNotIn("set +e", body)  # the step fails when the drill exits non-zero: 1 means it did not pass, 2 means it could not run
        self.assertNotIn("|| true", body)
        self.assertNotIn("continue-on-error", body)
        self.assertNotIn("make up", body)  # it does not start the stack


if __name__ == "__main__":
    unittest.main()
