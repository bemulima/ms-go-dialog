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
		rg -q '^schema_version: 1$$' "$$file"; \
	done

migrate:
	@set -eu; \
	for file in $$(find db/migrations -name '*.up.sql' | sort); do \
		docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U "$${POSTGRES_USER:-postgres}" -d "$${POSTGRES_DB:-ms_dialog}" < "$$file"; \
	done

up:
	docker compose up -d --build

down:
	docker compose down -v
