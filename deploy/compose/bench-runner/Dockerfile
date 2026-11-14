# The benchmark runner's image (ENV-09; PERD §65.1: the bench profile holds "the hostility proxy and runner"). SYNTHETIC test tooling, not a Noema deployable
# (DEC-2026-10-16 E5). It holds the pinned Python dependencies of benchmark/requirements.lock and nothing else: the benchmark code, the pack and the contracts are
# mounted read-only by services/bench-runner.yml, and benchmark-heldout is never mounted (INV-22).
FROM python:3.13.16-slim-bookworm@sha256:a1165e272e578941b84abc79e4ab38a0305cd12803a5c4247979ac7655f4d641
COPY benchmark/requirements.lock /tmp/requirements.lock
RUN pip install --no-cache-dir --require-hashes -r /tmp/requirements.lock && rm /tmp/requirements.lock
WORKDIR /repo
USER 65532:65532
