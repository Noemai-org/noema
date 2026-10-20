#!/usr/bin/env python3
"""Image file-list check (PLT-02; DEC-2026-10-13 R2).

An image carries only its own service. Usage:
  check_image_files.py <service> --image <tag>      export the image and check it
  check_image_files.py <service> --list <file>      check a file listing (one path per line)
"""
import argparse
import subprocess
import sys

SERVICES = ("core", "model", "ui")
# Path fragments that mark another service or repository-level material. The model image may hold
# its own generated contracts package (app/noema_model/contracts, PERD section 64.2) but never the
# repository-level contracts directory (app/contracts, contracts/schemas).
FOREIGN = {
    "core": ["noema_model", "usr/share/nginx", "app/requirements.lock", "benchmark", "benchmark-heldout", "contracts/"],
    "model": ["usr/local/bin/core", "usr/share/nginx", "services/core", "benchmark", "benchmark-heldout", "app/contracts/", "contracts/schemas/"],
    "ui": ["noema_model", "usr/local/bin/core", "services/", "benchmark", "benchmark-heldout", "contracts/"],
}
SOURCE_SUFFIXES = {"core": (".go", "go.mod"), "model": (), "ui": (".ts",)}


def violations(paths: list[str], service: str) -> list[str]:
    found = []
    for path in paths:
        norm = path.strip().lstrip("./")
        for marker in FOREIGN[service]:
            if marker in norm:
                found.append(f"{norm}: belongs outside the {service} image (matches '{marker}')")
        if norm.endswith(SOURCE_SUFFIXES[service]) and SOURCE_SUFFIXES[service]:
            found.append(f"{norm}: source file shipped in the {service} image")
    return found


def export_paths(image: str) -> list[str]:
    cid = subprocess.run(["docker", "create", image], capture_output=True, text=True, check=True).stdout.strip()
    try:
        exported = subprocess.run(["docker", "export", cid], capture_output=True, check=True).stdout
    finally:
        subprocess.run(["docker", "rm", cid], capture_output=True)
    listing = subprocess.run(["tar", "-t"], input=exported, capture_output=True, check=True).stdout
    return [line for line in listing.decode().splitlines() if not line.endswith("/")]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("service", choices=SERVICES)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--image")
    group.add_argument("--list")
    args = parser.parse_args()
    if args.list:
        with open(args.list) as handle:
            paths = handle.read().splitlines()
    else:
        paths = export_paths(args.image)
    found = violations(paths, args.service)
    for item in found:
        print("image-files FAIL: " + item)
    if not found:
        print(f"image-files OK: {args.service} ({len(paths)} files)")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
