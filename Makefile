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

# mock-case (ENV-08): the synthetic case system. Tests run against a real PostgreSQL; scripts/test.sh starts a
# throwaway server unless MOCK_CASE_TEST_DATABASE_URL points at one.
env-mock-case-test:
	synthetic/mock_case/scripts/test.sh -race ./...
	cd synthetic/mock_case && test -z "$$(gofmt -l .)" && go vet ./...

env-mock-case-build:
	docker build -t noema-mock-case:dev synthetic/mock_case
