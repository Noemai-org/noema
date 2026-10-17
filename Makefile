# Targets for local work and CI. Python commands use a venv in .venv.
.PHONY: build test lint arch-lint up reset bench-smoke contracts-validate

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

arch-lint:
	python3 -I tools/lint/run.py

up:
	@echo "not yet: PLT-04"; exit 1

reset:
	@echo "not yet: PLT-04"; exit 1

bench-smoke:
	@echo "not yet: PLT-04"; exit 1

contracts-validate:
	@echo "not yet: CON-01"; exit 1
