ifneq (,$(wildcard ./.env))
include .env
export
endif

.PHONY: run test build migrate migrate-down migrate-status migration

GO := go
MIG_DIR := ./migrations

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./...

build:
	$(GO) build ./...

migrate:
	GOOSE_DRIVER=sqlite3 GOOSE_DBSTRING=$(DB_PATH) \
	GOOSE_MIGRATION_DIR=$(MIG_DIR) GOOSE_TABLE="migrations" \
	goose up -allow-missing

migrate-down:
	GOOSE_DRIVER=sqlite3 GOOSE_DBSTRING=$(DB_PATH) \
	GOOSE_MIGRATION_DIR=$(MIG_DIR) GOOSE_TABLE="migrations" \
	goose down -allow-missing

migrate-status:
	GOOSE_DRIVER=sqlite3 GOOSE_DBSTRING=$(DB_PATH) \
	GOOSE_MIGRATION_DIR=$(MIG_DIR) GOOSE_TABLE="migrations" \
	goose status

migration:
	test -n "$(name)" || (echo "Error: name variable is not set. Usage: make migration name=<migration_name>"; exit 1)
	goose -dir $(MIG_DIR) create $(name) sql
