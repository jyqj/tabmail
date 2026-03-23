.PHONY: build build-migrate run dev test vet lint web-lint web-test web-build contract-check check migrate migrate-status migrate-down backup-db restore-db backup-obj backup-obj-s3 restore-obj restore-obj-s3 docker-up docker-down clean

BINARY  := tabmail
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
GO      ?= go
GOENV   ?= GOSUMDB=sum.golang.org
GORUN   := env $(GOENV) $(GO)

build:
	CGO_ENABLED=0 $(GORUN) build $(LDFLAGS) -o bin/$(BINARY) ./cmd/tabmail

build-migrate:
	CGO_ENABLED=0 $(GORUN) build $(LDFLAGS) -o bin/tabmail-migrate ./cmd/tabmail-migrate

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
	$(GORUN) run ./cmd/tabmail-migrate up $(if $(TO),-to $(TO),)

migrate-status:
	$(GORUN) run ./cmd/tabmail-migrate status

migrate-down:
	$(GORUN) run ./cmd/tabmail-migrate down -steps $(or $(STEPS),1)

backup-db:
	./scripts/backup_postgres.sh $(FILE)

restore-db:
	./scripts/restore_postgres.sh "$(FILE)"

backup-obj:
	if [ "$${TABMAIL_OBJECTSTORE:-fs}" = "s3" ]; then ./scripts/backup_s3_objectstore.sh; else ./scripts/backup_files_objectstore.sh; fi

backup-obj-s3:
	./scripts/backup_s3_objectstore.sh

restore-obj:
	if [ "$${TABMAIL_OBJECTSTORE:-fs}" = "s3" ]; then ./scripts/restore_s3_objectstore.sh "$(FILE)"; else ./scripts/restore_files_objectstore.sh "$(FILE)"; fi

restore-obj-s3:
	./scripts/restore_s3_objectstore.sh "$(FILE)"

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -rf bin/
