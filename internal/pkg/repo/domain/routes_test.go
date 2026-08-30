package domainrepo

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"tunnelmanager/internal/model"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

func TestDomainAggregateCreateReadAndReplaceRoutes(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/repo.db?mode=rwc&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	bunDB := bun.NewDB(db, sqlitedialect.New())
	defer bunDB.Close()
	ctx := context.Background()
	if _, err := bunDB.ExecContext(ctx, `CREATE TABLE domains (
		id TEXT PRIMARY KEY, hostname TEXT NOT NULL UNIQUE, origin_url TEXT NOT NULL,
		cloudflare_zone_id TEXT NOT NULL, cloudflare_tunnel_id TEXT NOT NULL,
		dns_record_id TEXT NOT NULL, tunnel_token TEXT NOT NULL, status TEXT NOT NULL,
		metrics_port INTEGER NOT NULL, pid INTEGER NOT NULL DEFAULT 0,
		restart_count INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := bunDB.ExecContext(ctx, `CREATE TABLE domain_routes (
		id TEXT PRIMARY KEY, domain_id TEXT NOT NULL, path TEXT NOT NULL,
		origin_url TEXT NOT NULL, strip_prefix INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE,
		UNIQUE(domain_id, path)
	)`); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	domain := &model.Domain{
		ID: "domain-1", Hostname: "app.example.com", OriginURL: "http://localhost:3000",
		CloudflareZoneID: "zone", CloudflareTunnelID: "tunnel", DNSRecordID: "record",
		EncryptedTunnelToken: "token", Status: "stopped", MetricsPort: 20500,
		CreatedAt: now, UpdatedAt: now,
		Routes: []model.DomainRoute{
			{ID: "api", DomainID: "domain-1", Path: "/api", OriginURL: "http://localhost:8080", StripPrefix: true, CreatedAt: now, UpdatedAt: now},
			{ID: "root", DomainID: "domain-1", Path: "/", OriginURL: "http://localhost:3000", CreatedAt: now, UpdatedAt: now},
		},
	}
	repo := NewRepository(bunDB)
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatal(err)
	}
	routes, err := repo.ListRoutes(ctx, domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Path != "/api" || routes[1].Path != "/" {
		t.Fatalf("routes = %#v", routes)
	}

	replacement := []model.DomainRoute{{ID: "root-2", DomainID: domain.ID, Path: "/", OriginURL: "http://localhost:4000", CreatedAt: now, UpdatedAt: now}}
	if err := repo.ReplaceRoutes(ctx, domain.ID, "http://localhost:4000", replacement); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginURL != "http://localhost:4000" {
		t.Fatalf("origin = %q", got.OriginURL)
	}
	byDomain, err := repo.ListRoutesByDomainIDs(ctx, []string{domain.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(byDomain[domain.ID]) != 1 || byDomain[domain.ID][0].ID != "root-2" {
		t.Fatalf("batch routes = %#v", byDomain)
	}
}
