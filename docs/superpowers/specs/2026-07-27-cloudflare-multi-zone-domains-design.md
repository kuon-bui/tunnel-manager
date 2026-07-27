# Cloudflare Multi-Zone Domains Design

**Date:** 2026-07-27

## Goal

Allow one backend instance to create and manage tunnel hostnames across multiple active Cloudflare DNS zones in one Cloudflare account. Frontend lists available base domains, user chooses one zone, then domain creation persists and reuses that zone ID.

## Current Constraint

Backend already manages multiple hostname rows, but all DNS operations use one global `CLOUDFLARE_ZONE_ID`. Every row remains one hostname, one tunnel, one DNS record, and one local `cloudflared` process.

## Scope

### Included

- List active zones available to `CLOUDFLARE_ACCOUNT_ID`.
- Require frontend to send selected `zoneId` when creating a domain.
- Validate selected zone and hostname ownership server-side.
- Persist Cloudflare zone ID on every domain row.
- Use persisted zone ID for DNS creation, rollback, and deletion.
- Remove `CLOUDFLARE_ZONE_ID` from runtime configuration.
- Reset existing domain data through a destructive schema migration; no data backfill.

### Excluded

- Multiple Cloudflare accounts or API tokens.
- Multiple hostnames on one tunnel.
- Importing existing tunnels or DNS records.
- Zone cache, zone synchronization table, or background refresh.
- Path-based ingress, wildcard ingress, and configurable catch-all rules.
- Frontend changes.

## Architecture

Keep current dependency flow:

```text
HTTP route/handler
  -> domain service
  -> Cloudflare client
  -> Cloudflare API
```

Extend existing domain module because zone listing exists only to support domain creation. Do not add a repository or database table for zones. Cloudflare remains zone source of truth.

Add zone operations to `internal/pkg/cloudflare.CloudflareClient`:

```go
ListZones(ctx context.Context) ([]model.CloudflareZone, error)
CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (string, error)
DeleteDNSRecord(ctx context.Context, zoneID, dnsRecordID string) error
```

Add `ListCloudflareZones(ctx context.Context)` to domain service. Handler calls service rather than infrastructure client directly.

## Cloudflare Zone Discovery

Cloudflare supports listing zones through `GET /zones`, filtered by account ID. Client must fetch every result page, filter zones with status `active`, and sort by normalized zone name ascending.

API token permissions must include:

- Account-scoped Cloudflare Tunnel write permissions already required by tunnel operations.
- `Zone Read` to list and validate zones.
- `DNS Edit` for every selectable zone.

No zone cache. Each zone-list request and domain-create validation reads current Cloudflare data. Add cache only if measured latency or rate limits become a problem.

## HTTP API

All endpoints remain JWT-protected.

### List selectable base domains

`GET /api/cloudflare/zones`

Success: `200 OK`

```json
{
  "items": [
    {
      "id": "023e105f4ecef8ad9ca31a8372d0c353",
      "name": "example.com",
      "status": "active"
    }
  ]
}
```

Contract:

- Return only active zones belonging to configured `CLOUDFLARE_ACCOUNT_ID`.
- Return all matching zones; no backend API pagination in this scope.
- Sort items by `name` ascending for stable frontend display.
- Do not expose Cloudflare SDK errors or credentials.
- Return `502 Bad Gateway` when Cloudflare listing fails.

### Create domain

`POST /api/domains`

Request:

```json
{
  "hostname": "app.example.com",
  "originUrl": "http://app:8080",
  "zoneId": "023e105f4ecef8ad9ca31a8372d0c353"
}
```

`zoneId` is required. Requests using old `{hostname, originUrl}` shape fail with `400 Bad Request`.

Response preserves existing domain fields and adds:

```json
{
  "zoneId": "023e105f4ecef8ad9ca31a8372d0c353"
}
```

Existing list and get responses include `zoneId` because they serialize persisted domain models.

## Validation

At create boundary:

1. Require non-empty `hostname`, `originUrl`, and `zoneId`.
2. Normalize hostname by trimming whitespace, converting ASCII letters to lowercase, and removing one trailing dot.
3. Load active zones from Cloudflare for configured account.
4. Find exact requested zone ID in returned active zones.
5. Accept hostname only when it equals zone name or ends with `.` plus zone name.
6. Reject all other hostname-zone combinations.

Boundary check prevents false suffix matches: `notexample.com` does not belong to `example.com`.

This change does not add IDN conversion or full DNS label validation. Add `golang.org/x/net/idna` and stricter validation only when internationalized domain support is requested. Origin URL behavior remains unchanged.

Validation failures:

- Missing fields: `400 Bad Request`.
- Zone ID absent from successful active-zone result: `400 Bad Request`.
- Hostname outside selected zone: `400 Bad Request`.
- Cloudflare list/auth/rate-limit/network failure: `502 Bad Gateway`.

## Data Model

Add model:

```go
type CloudflareZone struct {
    ID     string `json:"id"`
    Name   string `json:"name"`
    Status string `json:"status"`
}
```

Add domain field:

```go
CloudflareZoneID string `bun:"cloudflare_zone_id,notnull" json:"zoneId"`
```

Keep `hostname` globally unique. Multi-zone support does not require duplicate fully qualified hostnames.

## Migration and Data Reset

Add Goose migration `00004_recreate_domains_with_zone_id.sql`.

Up migration:

1. Drop `domains`.
2. Recreate `domains` with existing columns and constraints.
3. Add `cloudflare_zone_id TEXT NOT NULL`.

Down migration recreates original schema without `cloudflare_zone_id`. Both directions discard domain data.

This migration intentionally destroys all local domain rows, including encrypted tunnel tokens and local process metadata. It does not delete remote Cloudflare tunnels or DNS records. Operator must remove unwanted remote resources before migration to avoid orphans.

After migration, startup reconcile finds no domains. New domains must be recreated through API.

Remove `CLOUDFLARE_ZONE_ID` from:

- `internal/pkg/config.Config`
- config loading and validation
- `.env.example`
- operator setup in `README.md`

Keep `CLOUDFLARE_ACCOUNT_ID`; tunnel operations and zone filtering still require it.

## Create Flow

1. Handler binds `hostname`, `originUrl`, and `zoneId`.
2. Domain service lists active account zones.
3. Service validates selected zone and hostname boundary.
4. Service checks normalized hostname uniqueness.
5. Service creates tunnel and obtains token.
6. Service writes one hostname ingress plus `http_status:404` catch-all.
7. Service creates proxied CNAME using selected zone ID.
8. Service encrypts tunnel token, allocates metrics port, and persists domain with zone ID.
9. Service starts one local process for new tunnel.

Rollback uses selected zone ID when deleting DNS. Existing rollback order remains DNS then tunnel.

## Delete Flow

1. Load domain row.
2. Stop local process when running.
3. Delete DNS record using `domain.CloudflareZoneID`.
4. Delete tunnel using configured account ID.
5. Delete domain row.

This scope preserves existing best-effort Cloudflare cleanup semantics. Reliable deletion retries and orphan tracking need separate design because they affect resource lifecycle beyond multi-zone support.

## Error Handling

Cloudflare adapter wraps operations with context but never includes credentials. Service distinguishes invalid user selection from upstream failure:

- Successful zone list with no requested active zone means invalid input.
- Failed zone list means upstream failure.

Zone-list handler maps upstream failure to `502`. Domain-create handler maps validation failures to `400` and zone-list upstream failures to `502`. Existing create failures unrelated to zone discovery keep current behavior unless required for this distinction.

## Testing

Use Go standard `testing` package and fakes; add no test framework.

### Cloudflare client tests

- Fetch all zone pages for configured account.
- Return only active zones.
- Sort zones by name.
- Pass explicit zone ID to DNS create.
- Pass persisted zone ID to DNS delete.

### Domain service tests

- Persist selected zone ID on successful create.
- Normalize hostname before duplicate check and persistence.
- Accept zone apex.
- Accept proper subdomain.
- Reject unrelated suffix such as `notexample.com` for `example.com`.
- Reject inactive, inaccessible, or wrong-account zone by absence from active result.
- Map Cloudflare zone-list failure separately from invalid selection.
- Use selected zone ID during DNS rollback.
- Use persisted zone ID during delete.

### Route tests

- Require JWT for zone listing.
- Return `{items:[...]}` shape.
- Return `502` for Cloudflare zone-list failure.
- Reject create without `zoneId` using `400`.
- Return `zoneId` in create/get/list domain JSON.

### Migration and verification

- Apply migrations to a temporary SQLite database and assert `domains.cloudflare_zone_id` is `NOT NULL`.
- Run `go test ./...`.
- Run `go build ./...`.

## Rollout

1. Manually remove old Cloudflare tunnels and DNS records that should not survive.
2. Stop backend to prevent new writes during reset.
3. Update environment by removing `CLOUDFLARE_ZONE_ID`; ensure API token has `Zone Read` and `DNS Edit` across target zones.
4. Deploy backend binary and run `make migrate` before startup.
5. Start backend.
6. Verify `GET /api/cloudflare/zones` returns expected active zones.
7. Recreate domains using selected zone IDs.

Rollback through Goose down also discards newly recreated domain rows. Treat rollback as destructive and clean remote resources deliberately.

## Success Criteria

- One backend instance lists all active zones in configured Cloudflare account.
- Frontend can select a returned zone ID and create hostname in that zone.
- Backend rejects mismatched hostname and zone.
- DNS create, rollback, and delete always use domain-specific persisted zone ID.
- Existing one-hostname-per-tunnel lifecycle remains unchanged.
- No runtime dependency on `CLOUDFLARE_ZONE_ID` remains.
