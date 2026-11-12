#!/usr/bin/env python3
"""Splits the Go packages of services/core into balanced test shards for the core workflow (CI speed).

Usage: go list ./... | go_shards.py --shards N --index I [--weights go_shard_weights.json]   (prints the packages of shard I, space-separated)

Longest-processing-time first: packages are sorted by weight (seconds, from the weights file; unknown packages count 2) and each goes to the currently lightest shard, ties broken by
name, so the result does not depend on input order. Every package lands in exactly one shard: the weights only decide the balance, never what runs."""
import argparse
import json
import sys
from pathlib import Path

DEFAULT = 2
PREFIX = "github.com/Noemai-org/noema/services/core/"


def shard(packages, shards, index, weights):
    """The packages of shard `index` of `shards`. `weights` maps a package (full import path or the path after services/core/) to seconds."""
    def weight(p):
        return weights.get(p, weights.get(p.removeprefix(PREFIX), DEFAULT))
    loads = [0] * shards
    mine = []
    for p in sorted(set(packages), key=lambda p: (-weight(p), p)):
        i = min(range(shards), key=lambda k: (loads[k], k))
        loads[i] += weight(p)
        if i == index:
            mine.append(p)
    return sorted(mine)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--shards", type=int, required=True)
    ap.add_argument("--index", type=int, required=True)
    ap.add_argument("--weights", type=Path)
    a = ap.parse_args()
    if a.shards < 1 or not 0 <= a.index < a.shards:
        print("go_shards: --shards must be at least 1 and --index in [0, shards)", file=sys.stderr)
        return 2
    weights = json.loads(a.weights.read_text())["seconds"] if a.weights else {}
    packages = [line.strip() for line in sys.stdin if line.strip()]
    if not packages:
        print("go_shards: no packages on stdin", file=sys.stderr)
        return 2
    print(" ".join(shard(packages, a.shards, a.index, weights)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
