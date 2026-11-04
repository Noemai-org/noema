# Targets for local work and CI. Python commands use a venv in .venv.
.PHONY: build test lint arch-lint up down net-matrix reset bench-smoke contracts-validate codegen codegen-check env-mock-case-test env-mock-case-build env-relay-test env-relay-build env-proxy-test env-proxy-build model-bench-up

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
	python3 deploy/compose/netcheck/test_ten01.py
	python3 deploy/compose/netcheck/test_profiles.py
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
	.venv-contracts/bin/python tools/codegen/shape_refusals.py --check

# Generated Go and Pydantic types (CON-03). `make codegen` rewrites them from contracts/schemas; `make codegen-check` is what CI runs.
codegen:
	python3 -I tools/codegen/generate.py --write

codegen-check:
	python3 -I tools/codegen/generate.py --check
	python3 -I -m unittest discover -s tools/codegen -p 'test_*.py'

# Benchmark (session C; T4 of DEC-2026-10-20). Targets are prefixed bench-.
.PHONY: bench-test bench-venv bench bench-live bench-us bench-chaos bench-evil bench-full bench-g1
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

# Hostility proxy (ENV-09): the Go standard library only, so its tests need no database.
env-proxy-test:
	cd benchmark/hostility_proxy && test -z "$$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./...

env-proxy-build:
	docker build -t noema-hostility-proxy:dev benchmark/hostility_proxy

# make bench: the generated layer end to end (T4 of DEC-2026-10-20). Generate the suite (a coverage gap fails here), say what this build can run, run it, score it.
# The environment comes in through BENCH_RUNNER_ARGS (see `python -m benchmark.runner --help`). Without it, or without a core and a fixture loader, the run is
# "not run: no access" and exits 3: nothing is faked and nothing is scored. Exit 0: complete and Tier 0 all zero. 1: incomplete, or a gate is not zero. 2: bad input.
bench: bench-venv
	$(BENCH_PY) -m benchmark.generator generate --seed $(BENCH_SEED) --out $(BENCH_OUT)/suite
	$(BENCH_PY) -m benchmark.runner --suite $(BENCH_OUT)/suite --out $(BENCH_OUT)/run --support $(BENCH_RUNNER_ARGS)
	rm -rf $(BENCH_OUT)/run $(BENCH_OUT)/score
	$(BENCH_PY) -m benchmark.runner --suite $(BENCH_OUT)/suite --out $(BENCH_OUT)/run --run-id $(BENCH_RUN_ID) --cost-dir $(BENCH_OUT)/cost $(BENCH_RUNNER_ARGS); rc=$$?; \
	if [ $$rc -eq 3 ]; then echo "make bench: not run, no access (see $(BENCH_OUT)/run/run-manifest.json)"; exit 3; fi; \
	if [ $$rc -ge 2 ]; then exit $$rc; fi; \
	$(BENCH_PY) -m benchmark.scoring --cases $(BENCH_OUT)/suite/cases --oracles $(BENCH_OUT)/suite/oracle --results $(BENCH_OUT)/run/results \
	  --pack packs/fintech_v1.json --manifest $(BENCH_OUT)/suite/manifest.json --run-id $(BENCH_RUN_ID) --out $(BENCH_OUT)/score

# The oracle client and every translated fault plan against a real mock-case and PostgreSQL. Needs go and PostgreSQL server binaries on this machine.
bench-live: bench-venv
	$(BENCH_PY) -m benchmark.oracle_client.live_check

# BEN-29: one command per named suite (benchmark/suites/suites.json): us, chaos, evil, full. Each starts with `make reset` (Docker), generates the suite, runs the selected
# cases through the public API, scores exactly those cases and records the run, with its run ID, in $(BENCH_OUT)/<suite>/suite-run.json.
# BENCH_SUITE_RUN_ID names the run (default: the suite name and the UTC time). BENCH_RESET=0 skips `make reset`; the record then says reset_done false.
# Exit codes as `make bench`: 0 complete and Tier 0 all zero, 1 incomplete or a gate not zero, 2 bad input, 3 not run, no access. The held-out layer is never run here.
BENCH_RESET ?= 1
BENCH_SUITE_ARGS ?=
bench-us bench-chaos bench-evil bench-full: bench-venv
	@suite=$(patsubst bench-%,%,$@); \
	if [ "$(BENCH_RESET)" = "1" ]; then $(MAKE) reset || exit $$?; reset_flag="--reset-done"; else reset_flag=""; fi; \
	run_id="$${BENCH_SUITE_RUN_ID:-$$suite-$$(date -u +%Y%m%dT%H%M%SZ)}"; \
	$(BENCH_PY) -m benchmark.generator generate --seed $(BENCH_SEED) --out $(BENCH_OUT)/suite > /dev/null || exit $$?; \
	$(BENCH_PY) -m benchmark.suites run $$suite --suite $(BENCH_OUT)/suite --out $(BENCH_OUT)/$$suite --run-id "$$run_id" $$reset_flag -- $(BENCH_SUITE_ARGS)

# BEN-05, the G1 evidence run: the g1 suite (every case covering US-01 or US-06) twice, each leg from `make reset` with its own run ID and the same commit, then the replay diff
# (benchmark/replay, noema#102) of the Trust decisions and verdicts with the named open exclusions. Needs OUT-01 and REC-03 on main and the environment; until then each leg is
# NOT_RUN (exit 3) and the target stops. BENCH_G1_ID names the pair (default g1-<UTC date>); the legs are <id>-a and <id>-b. A rerun is a new pair with its own ID.
BENCH_G1_ID ?= g1-$(shell date -u +%Y%m%d-%H%M%S)
# Where the scenario data comes from. G1 runs on the Noema-side stand-in (Founder's decision of 4 Nov 2026) and the evidence is labelled so; "tushe" only once its OpenAPI is frozen.
BENCH_G1_DATA_SOURCE ?= stand-in
bench-g1: bench-venv
	@$(BENCH_PY) -m benchmark.generator generate --seed $(BENCH_SEED) --out $(BENCH_OUT)/g1/suite > /dev/null || exit $$?; \
	reset_flag=""; if [ "$(BENCH_RESET)" = "1" ]; then reset_flag="--reset"; fi; \
	$(BENCH_PY) -m benchmark.suites g1 --suite $(BENCH_OUT)/g1/suite --out $(BENCH_OUT)/g1 --run-id "$(BENCH_G1_ID)" --commit-sha "$$(git rev-parse HEAD)" --data-source "$(BENCH_G1_DATA_SOURCE)" $$reset_flag -- $(BENCH_SUITE_ARGS)

# webhook relay (ENV-13): standard library only; no database or network needed.
env-relay-test:
	cd synthetic/webhook_relay && test -z "$$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./...

env-relay-build:
	docker build -t noema-webhook-relay:dev synthetic/webhook_relay

# Model service (session D): the bench stack with the model writing its cost records to $(BENCH_OUT)/cost/ (REQ-NFR-007).
# Only this target adds deploy/compose/bench-model.yml, so `make up` never sets BENCH_OUT or mounts anything writable.
model-bench-up:
	deploy/compose/secrets_local.sh
	mkdir -p $(BENCH_OUT)
	NOEMA_BENCH_OUT=$(abspath $(BENCH_OUT)) NOEMA_BENCH_UID=$$(id -u) NOEMA_BENCH_GID=$$(id -g) \
	  docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/bench-model.yml --profile bench up -d --build --wait
