#!/bin/sh
set -eu

mkdir -p "$(dirname "$DB_PATH")" "$LOG_DIR"

migrate() {
	GOOSE_DRIVER=sqlite3 \
	GOOSE_DBSTRING="$DB_PATH" \
	GOOSE_MIGRATION_DIR=/app/migrations \
	GOOSE_TABLE=migrations \
	/usr/local/bin/goose up -allow-missing
}

# `docker-entrypoint.sh migrate` only applies migrations, so a deploy can run
# them as a separate step and stop before starting a server on a bad schema.
if [ "${1:-}" = "migrate" ]; then
	migrate
	exit 0
fi

# Default: migrate (a no-op when already applied), then start the server.
migrate
exec /usr/local/bin/tunnel-manager
