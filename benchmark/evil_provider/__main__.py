"""Run the evil provider: python -m evil_provider (in the container) or python -m benchmark.evil_provider.

Configuration, all from the environment, nothing from a request:
  NOEMA_PROFILE           must be `bench`; the server refuses to start under any other profile
  EVIL_PROVIDER_ADDR      host:port to listen on, default 0.0.0.0:8090
  EVIL_PROVIDER_MODE      one of the attack modes, default `all`
  EVIL_PROVIDER_SEED      integer seed, default 1
  EVIL_PROVIDER_SCRIPT    path of a JSON file {question id or "*": option} that pins decision answers (optional)
"""
import json
import os
import sys
from pathlib import Path

from .adversary import MODES, Config
from .server import make_server


def _log(line: dict[str, object]) -> None:
    print(json.dumps(line, sort_keys=True), flush=True)


def main() -> int:
    if os.environ.get("NOEMA_PROFILE") != "bench":
        print("refused: the evil provider runs only under NOEMA_PROFILE=bench")
        return 2
    mode = os.environ.get("EVIL_PROVIDER_MODE", "all")
    if mode not in MODES:
        print(f"refused: EVIL_PROVIDER_MODE {mode!r} is not one of {MODES}")
        return 2
    try:
        seed = int(os.environ.get("EVIL_PROVIDER_SEED", "1"))
        host, _, port = os.environ.get("EVIL_PROVIDER_ADDR", "0.0.0.0:8090").rpartition(":")
        script_path = os.environ.get("EVIL_PROVIDER_SCRIPT")
        script = json.loads(Path(script_path).read_text()) if script_path else {}
        if not isinstance(script, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in script.items()):
            raise ValueError("the script is a JSON object of strings")
        server = make_server(host or "0.0.0.0", int(port), Config(mode=mode, seed=seed), script, _log)
    except (ValueError, OSError) as exc:
        print(f"refused: {exc}")
        return 2
    _log({"event": "listening", "mode": mode, "seed": seed, "address": f"{host}:{port}"})
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
