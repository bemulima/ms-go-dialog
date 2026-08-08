.PHONY: fmt deps tidy test lint validate-contracts migrate up down

fmt:
	gofmt -w cmd internal test

deps:
	XDG_CACHE_HOME=$(CURDIR)/.cache GOMODCACHE=$(CURDIR)/.cache/gomod go mod download all

tidy:
	XDG_CACHE_HOME=$(CURDIR)/.cache GOMODCACHE=$(CURDIR)/.cache/gomod go mod tidy

test:
	XDG_CACHE_HOME=$(CURDIR)/.cache GOCACHE=$(CURDIR)/.cache/go-build GOMODCACHE=$(CURDIR)/.cache/gomod go test ./...

lint:
	$(CURDIR)/.cache/bin/golangci-lint run ./...

validate-contracts:
	@set -eu; \
	for file in .ai/service.yaml .ai/architecture.yaml .ai/commands.yaml .ai/contracts/database.yaml .ai/contracts/http.yaml .ai/contracts/websocket.yaml .ai/contracts/events.yaml .ai/contracts/frontend.yaml; do \
		test -s "$$file"; \
		grep -q '^schema_version: 1$$' "$$file"; \
	done
	@XDG_CACHE_HOME=$(CURDIR)/.cache GOCACHE=$(CURDIR)/.cache/go-build GOMODCACHE=$(CURDIR)/.cache/gomod go test ./test/contracts

migrate:
	@sh scripts/migrate.sh

up:
	docker compose up -d --build

down:
	docker compose down -v
