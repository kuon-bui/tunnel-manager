package migrations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDomainZoneMigrationResetsSchema(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "migration.db")+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, migrationSection(t, "00001_create_domains.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO domains (
		id, hostname, origin_url, cloudflare_tunnel_id, dns_record_id,
		tunnel_token, status, metrics_port, pid, restart_count, last_error,
		created_at, updated_at
	) VALUES ('old', 'old.example.com', 'http://localhost:8080', 'tunnel',
		'record', 'token', 'stopped', 20500, 0, 0, '', CURRENT_TIMESTAMP,
		CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, migrationSection(t, "00004_recreate_domains_with_zone_id.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("domain count = %d, want 0", count)
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(domains)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "cloudflare_zone_id" {
			found = true
			if columnType != "TEXT" || notNull != 1 {
				t.Fatalf("cloudflare_zone_id type/notnull = %s/%d", columnType, notNull)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("cloudflare_zone_id column missing")
	}
}

func TestDomainRoutesMigrationPreservesAndBackfillsDomains(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "routes.db")+"?mode=rwc&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, migrationSection(t, "00001_create_domains.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, migrationSection(t, "00004_recreate_domains_with_zone_id.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO domains (
		id, hostname, origin_url, cloudflare_zone_id, cloudflare_tunnel_id,
		dns_record_id, tunnel_token, status, metrics_port, pid, restart_count,
		last_error, created_at, updated_at
	) VALUES ('domain-1', 'app.example.com', 'http://localhost:8080', 'zone-1',
		'tunnel-1', 'record-1', 'token', 'stopped', 20500, 0, 0, '',
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, migrationSection(t, "00005_create_domain_routes.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}

	var domainCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains").Scan(&domainCount); err != nil {
		t.Fatal(err)
	}
	if domainCount != 1 {
		t.Fatalf("domain count = %d, want 1", domainCount)
	}

	var path, origin string
	var strip bool
	if err := db.QueryRowContext(ctx, `SELECT path, origin_url, strip_prefix
		FROM domain_routes WHERE domain_id = 'domain-1'`).Scan(&path, &origin, &strip); err != nil {
		t.Fatal(err)
	}
	if path != "/" || origin != "http://localhost:8080" || strip {
		t.Fatalf("backfilled route = %q/%q/%v", path, origin, strip)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO domain_routes (
		id, domain_id, path, origin_url, strip_prefix, created_at, updated_at
	) VALUES ('duplicate', 'domain-1', '/', 'http://localhost:9000', 0,
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("expected duplicate domain/path failure")
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM domains WHERE id = 'domain-1'"); err != nil {
		t.Fatal(err)
	}
	var routeCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domain_routes").Scan(&routeCount); err != nil {
		t.Fatal(err)
	}
	if routeCount != 0 {
		t.Fatalf("route count after domain delete = %d", routeCount)
	}
}

func migrationSection(t *testing.T, name, start, end string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	section, ok := strings.CutPrefix(string(contents), start)
	if !ok {
		t.Fatalf("%s missing %s", name, start)
	}
	section, _, ok = strings.Cut(section, end)
	if !ok {
		t.Fatalf("%s missing %s", name, end)
	}
	return section
}
