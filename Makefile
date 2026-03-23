.PHONY: build run dev test vet lint web-lint web-test web-build contract-check check migrate docker-up docker-down clean

BINARY  := tabmail
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
GO      ?= go
GOENV   ?= GOSUMDB=sum.golang.org
GORUN   := env $(GOENV) $(GO)

build:
	CGO_ENABLED=0 $(GORUN) build $(LDFLAGS) -o bin/$(BINARY) ./cmd/tabmail

run: build
	./bin/$(BINARY)

dev:
	$(GORUN) run ./cmd/tabmail

test:
	$(GORUN) test -race -count=1 ./cmd/... ./internal/...

vet:
	$(GORUN) vet ./cmd/... ./internal/...

web-lint:
	cd web && npm run lint

web-test:
	cd web && npm run test

web-build:
	cd web && npm run build

contract-check:
	python3 scripts/check_contract_drift.py

lint: vet web-lint

check: test vet contract-check web-lint web-test web-build

migrate:
	for f in migrations/*.sql; do psql "$(TABMAIL_DB_DSN)" -f "$$f"; done

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -rf bin/
