#!/bin/sh
set -eu

mkdir -p "$(dirname "$DB_PATH")" "$LOG_DIR"

GOOSE_DRIVER=sqlite3 \
GOOSE_DBSTRING="$DB_PATH" \
GOOSE_MIGRATION_DIR=/app/migrations \
GOOSE_TABLE=migrations \
/usr/local/bin/goose up -allow-missing

exec /usr/local/bin/tunnel-manager
