"""The raw-audio store never leaves the bench host (A451, A377 to A379; REQ-VOI-004, AGENT-13).

Raw audio lives only in a private store on the bench host: never in the repository, never in a CI artifact, never in a container this repository starts. These tests read the real workflows, the
real Compose files and the real purge units, and fail if any of them names the store's setting or uploads a path that could hold audio. The store's own checks are in test_store.sh (Linux)."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
SETTINGS = ("NOEMA_AUDIO_STORE_DIR", "NOEMA_AUDIO_RETENTION_DAYS")
AUDIO_FILE = re.compile(r"(?i)\.(wav|flac|mp3|ogg|opus|m4a|aac|webm)\b|\baudio\b|\brecordings?\b")


def workflows() -> list[Path]:
    return sorted((ROOT / ".github" / "workflows").glob("*.y*ml"))


def compose_files() -> list[Path]:
    return sorted((ROOT / "deploy" / "compose").glob("*.yml")) + sorted((ROOT / "deploy" / "compose").glob("**/*.yaml"))


class AudioNeverLeavesTheHost(unittest.TestCase):
    def test_there_are_files_to_read(self) -> None:
        self.assertGreaterEqual(len(workflows()), 10)
        self.assertGreaterEqual(len(compose_files()), 3)
        for name in ("store.sh", "noema-audio-purge.service", "noema-audio-purge.timer", "README.md"):
            self.assertTrue((HERE / name).is_file(), name)

    def test_no_workflow_names_the_store_or_its_retention_setting(self) -> None:
        for path in workflows():
            text = path.read_text(encoding="utf-8")
            for name in SETTINGS:
                self.assertNotIn(name, text, f"{path.name} names {name}: the store is not reachable from CI")

    def test_no_compose_file_names_the_store_or_mounts_an_audio_path(self) -> None:
        for path in compose_files():
            text = path.read_text(encoding="utf-8")
            for name in SETTINGS:
                self.assertNotIn(name, text, f"{path.name} names {name}: no container this repository starts holds the store")
            for line in text.splitlines():
                code = line.split("#", 1)[0]
                if re.search(r"^\s*-\s*\S+:\S+", code) or "source:" in code:
                    self.assertIsNone(AUDIO_FILE.search(code), f"{path.name}: a mount names audio: {line.strip()}")

    def test_no_artifact_upload_path_can_hold_audio(self) -> None:
        for path in workflows():
            text = path.read_text(encoding="utf-8")
            for block in re.finditer(r"(?ms)uses: actions/upload-artifact@\S+.*?path: \|?\s*\n?(.*?)(?=^\s+(?:if-no-files-found|retention-days|name|if|with):|\Z)", text):
                self.assertIsNone(AUDIO_FILE.search(block.group(1)), f"{path.name}: an uploaded path names audio")

    def test_the_purge_units_carry_no_path_and_no_number(self) -> None:
        # The store's path and its retention are values of the host (an environment file the units read), never of this repository (AGENT-16).
        service = (HERE / "noema-audio-purge.service").read_text(encoding="utf-8")
        self.assertIn("EnvironmentFile=", service)
        self.assertIn("ExecStart=", service)
        self.assertNotRegex(service, r"NOEMA_AUDIO_RETENTION_DAYS\s*=")
        self.assertNotRegex(service, r"NOEMA_AUDIO_STORE_DIR\s*=")
        # SR on noema#819: no fixed path in the unit (the store's path is the host's setting), and the unit is hardened for a job that only reads a directory and deletes files in it.
        self.assertNotIn("ReadWritePaths=", service)
        self.assertNotIn("/var/lib", service)
        for directive in ("NoNewPrivileges=true", "PrivateTmp=true", "PrivateNetwork=true", "PrivateDevices=true", "CapabilityBoundingSet=", "ProtectSystem=full", "ProtectKernelTunables=true",
                          "ProtectKernelModules=true", "ProtectControlGroups=true", "LockPersonality=true", "MemoryDenyWriteExecute=true", "RestrictRealtime=true", "RestrictSUIDSGID=true",
                          "SystemCallFilter=@system-service", "UMask=0077"):
            self.assertIn(directive, service, directive)
        timer = (HERE / "noema-audio-purge.timer").read_text(encoding="utf-8")
        self.assertRegex(timer, r"(?m)^OnCalendar=")
        self.assertIn("Persistent=true", timer)

    def test_the_store_script_has_no_default_for_either_setting(self) -> None:
        script = (HERE / "store.sh").read_text(encoding="utf-8")
        for name in SETTINGS:
            self.assertNotRegex(script, name + r":-", f"{name} has a default")
            self.assertNotRegex(script, name + r"=[\"']?[0-9/]", f"{name} is assigned a literal")

    def test_the_readme_states_who_reads_it_and_that_nothing_is_uploaded(self) -> None:
        text = (HERE / "README.md").read_text(encoding="utf-8")
        for fragment in ("0700", "never uploaded", "REQ-VOI-004", "Founders", "voice runner", "ANALYSIS", "90 days after analysis", "remove <speaker_ref>", "without a speaker_ref", "Encryption at rest"):
            self.assertIn(fragment, text, fragment)


if __name__ == "__main__":
    unittest.main()
