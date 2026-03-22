.PHONY: build run dev test lint migrate docker-up docker-down clean

BINARY  := tabmail
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/tabmail

run: build
	./bin/$(BINARY)

dev:
	go run ./cmd/tabmail

test:
	go test -race -count=1 ./cmd/... ./internal/...

lint:
	golangci-lint run ./...

migrate:
	for f in migrations/*.sql; do psql "$(TABMAIL_DB_DSN)" -f "$$f"; done

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -rf bin/
