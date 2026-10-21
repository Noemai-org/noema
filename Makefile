# Targets for local work and CI. Python commands use a venv in .venv.
.PHONY: build test lint arch-lint up down net-matrix reset bench-smoke contracts-validate env-mock-case-test env-mock-case-build

build:
	docker build -t noema-core:dev services/core
	docker build -t noema-model:dev services/model
	docker build -t noema-ui:dev ui

test:
	cd services/core && go test ./...
	python3 -m venv .venv && .venv/bin/pip install -q --require-hashes -r services/model/requirements-dev.lock
	cd services/model && ../../.venv/bin/python -m pytest
	cd ui && npm ci --ignore-scripts && npm test

lint:
	cd services/core && test -z "$$(gofmt -l .)" && go vet ./...
	cd services/model && ../../.venv/bin/python -m mypy
	cd ui && npm run typecheck

COMPOSE := docker compose -f deploy/compose/docker-compose.yml --profile dev
arch-lint:
	python3 -I tools/lint/run.py

up:
	deploy/compose/secrets_local.sh
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

net-matrix:
	python3 -I deploy/compose/netcheck/matrix.py

reset:
	deploy/compose/reset.sh

bench-smoke:
	python3 -I benchmark/smoke.py

contracts-validate:
	python3 -m venv .venv-contracts
	.venv-contracts/bin/pip install -q --require-hashes -r tools/contracts/requirements.lock
	.venv-contracts/bin/python -m unittest discover -s services/core/orchestrator -p 'test_*.py'
	.venv-contracts/bin/python tools/contracts/validate.py
	.venv-contracts/bin/python -m unittest discover -s tools/contracts -p 'test_*.py'
	.venv-contracts/bin/python -m unittest discover -s packs/tests -p 'test_*.py'
	.venv-contracts/bin/python -m unittest discover -s tools/packcheck -p 'test_*.py'
	.venv-contracts/bin/python tools/packcheck validate packs/fintech_v1.json
	for p in packs/conformance/*.json; do .venv-contracts/bin/python tools/packcheck validate --conformance $$p || exit 1; done

# Benchmark (session C; T4 of DEC-2026-10-20). Targets are prefixed bench-.
.PHONY: bench-test bench-venv bench bench-live
BENCH_VENV := .venv-bench
BENCH_PY := $(BENCH_VENV)/bin/python
BENCH_SEED ?= 20261020
BENCH_OUT ?= benchmark/out
BENCH_RUN_ID ?= local
BENCH_RUNNER_ARGS ?=
bench-venv:
	python3 -m venv $(BENCH_VENV)
	$(BENCH_VENV)/bin/pip install -q --require-hashes -r benchmark/requirements-dev.lock

bench-test: bench-venv
	$(BENCH_VENV)/bin/python -m unittest discover -s benchmark -t . -p 'test_*.py'
	$(BENCH_VENV)/bin/python -m mypy -p benchmark --config-file benchmark/pyproject.toml

# mock-case (ENV-08): the synthetic case system. Tests run against a real PostgreSQL; scripts/test.sh starts a
# throwaway server unless MOCK_CASE_TEST_DATABASE_URL points at one.
env-mock-case-test:
	synthetic/mock_case/scripts/test.sh -race ./...
	cd synthetic/mock_case && test -z "$$(gofmt -l .)" && go vet ./...

env-mock-case-build:
	docker build -t noema-mock-case:dev synthetic/mock_case

# make bench: the generated layer end to end (T4 of DEC-2026-10-20). Generate the suite (a coverage gap fails here), say what this build can run, run it, score it.
# The environment comes in through BENCH_RUNNER_ARGS (see `python -m benchmark.runner --help`). Without it, or without a core and a fixture loader, the run is
# "not run: no access" and exits 3: nothing is faked and nothing is scored. Exit 0: complete and Tier 0 all zero. 1: incomplete, or a gate is not zero. 2: bad input.
bench: bench-venv
	$(BENCH_PY) -m benchmark.generator generate --seed $(BENCH_SEED) --out $(BENCH_OUT)/suite
	$(BENCH_PY) -m benchmark.runner --suite $(BENCH_OUT)/suite --out $(BENCH_OUT)/run --support $(BENCH_RUNNER_ARGS)
	rm -rf $(BENCH_OUT)/run $(BENCH_OUT)/score
	$(BENCH_PY) -m benchmark.runner --suite $(BENCH_OUT)/suite --out $(BENCH_OUT)/run --run-id $(BENCH_RUN_ID) $(BENCH_RUNNER_ARGS); rc=$$?; \
	if [ $$rc -eq 3 ]; then echo "make bench: not run, no access (see $(BENCH_OUT)/run/run-manifest.json)"; exit 3; fi; \
	if [ $$rc -ge 2 ]; then exit $$rc; fi; \
	$(BENCH_PY) -m benchmark.scoring --cases $(BENCH_OUT)/suite/cases --oracles $(BENCH_OUT)/suite/oracle --results $(BENCH_OUT)/run/results \
	  --pack packs/fintech_v1.json --manifest $(BENCH_OUT)/suite/manifest.json --run-id $(BENCH_RUN_ID) --out $(BENCH_OUT)/score

# The oracle client and every translated fault plan against a real mock-case and PostgreSQL. Needs go and PostgreSQL server binaries on this machine.
bench-live: bench-venv
	$(BENCH_PY) -m benchmark.oracle_client.live_check
