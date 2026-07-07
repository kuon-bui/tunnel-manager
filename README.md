# Tunnel Manager Backend

## Introduction

Tunnel Manager Backend is the control service behind a Cloudflare Tunnel based
domain routing setup. It provides a single HTTP API for provisioning tunnel
resources, mapping hostnames to upstream origins, and operating the
`cloudflared` processes that keep those routes online.

For an operator, this service is the place where three concerns meet:

- Cloudflare resource management
- local tunnel process supervision
- persisted runtime state and operational visibility

Instead of managing each tunnel manually in the Cloudflare dashboard and on the
host, you can use this service to keep tunnel lifecycle, DNS records, logs, and
metrics under one backend.

## Overview

Tunnel Manager Backend is a Go service built for operating Cloudflare
tunnel-backed domains through an internal API.

In practice, it takes a hostname and origin URL, creates the required
Cloudflare tunnel and DNS configuration, starts a managed `cloudflared`
process, and stores the resulting state in SQLite so that the service can
reconcile and continue operating across restarts.

At runtime it:

- creates and deletes Cloudflare tunnels and DNS records
- starts and stops `cloudflared` processes for managed domains
- stores service state in SQLite
- exposes operational endpoints under `/api/domains`
- proxies per-domain logs and Prometheus-style metrics for troubleshooting

This README is written for operators and deployers. It focuses on runtime
requirements, configuration, deployment order, and the exposed HTTP surface.

## What You Need Before Deploying

Prepare these dependencies before starting the service:

- A Cloudflare API token with access to the target account and zone
- The Cloudflare account ID and zone ID used by the service
- A 32-byte hex-encoded encryption key for stored tunnel tokens
- Writable storage for the SQLite database file and tunnel logs
- Network reachability to Cloudflare APIs
- The `cloudflared` binary on `PATH`, unless you use the provided `Dockerfile`

If you plan to use `make migrate`, make sure the `goose` CLI is installed and
available on `PATH`. The repository provides migration files and Make targets,
but it does not provide a separate migration container.

## Configuration

Use `.env.example` as the reference template for local or host-based
deployments. The `Makefile` automatically loads variables from `.env` when that
file is present.

Generate an encryption key with:

```bash
openssl rand -hex 32
```

### Environment Variables

| Variable | Required | Purpose |
| --- | --- | --- |
| `CLOUDFLARE_API_TOKEN` | Yes | API token used for Cloudflare tunnel and DNS operations. |
| `CLOUDFLARE_ACCOUNT_ID` | Yes | Cloudflare account that owns the tunnels. |
| `CLOUDFLARE_ZONE_ID` | Yes | Cloudflare DNS zone where CNAME records are created. |
| `ENCRYPTION_KEY` | Yes | Hex-encoded key used to encrypt stored tunnel tokens. Must decode to exactly 32 bytes. |
| `DB_PATH` | Yes | SQLite database path. Parent directory must be writable. |
| `LOG_DIR` | Yes | Directory where tunnel process logs are stored. Created on startup if missing. |
| `HTTP_ADDR` | Yes | HTTP listen address for the API server, for example `:8080`. |
| `METRICS_PORT_RANGE_START` | Yes | Start of the local metrics port range reserved for managed `cloudflared` processes. |
| `METRICS_PORT_RANGE_END` | Yes | End of the local metrics port range. Must be greater than `METRICS_PORT_RANGE_START`. |
| `CLOUDFLARED_BINARY` | Yes | Path or command name for the `cloudflared` executable. |
| `CLOUDFLARED_PROTOCOL` | Yes | Transport protocol passed to `cloudflared`. Allowed values: `http2`, `quic`. |

### Example Host Configuration

`.env.example` currently documents these defaults:

```env
DB_PATH=./data/tunnel-manager.db
LOG_DIR=./data/logs
HTTP_ADDR=:8080
METRICS_PORT_RANGE_START=20500
METRICS_PORT_RANGE_END=20999
CLOUDFLARED_BINARY=cloudflared
CLOUDFLARED_PROTOCOL=http2
```

For host deployments, ensure the `data/` directory exists and is writable
before running migrations or starting the service.

## Deployment Flow

Run deployment in this order:

1. Prepare environment variables and writable directories.
2. Run database migrations.
3. Start the service.
4. Verify the API is reachable.

### Host-Based Deployment

Populate a `.env` file using `.env.example` as the template, then run:

```bash
mkdir -p ./data/logs
make migrate
make run
```

Important notes:

- `make migrate` uses `DB_PATH` from `.env`.
- `make run` starts `go run ./cmd/server`.
- Starting the service before migrations is not recommended. The runtime expects
  the database path to exist and be accessible.

### Build a Binary

```bash
make build
go build -o ./tunnel-manager ./cmd/server
./tunnel-manager
```

`make build` is a compile check for all packages. Use the explicit `go build`
command above when you want a runnable binary artifact. The binary still
requires the same environment variables and access to `cloudflared`.

### Container Deployment

The provided `Dockerfile` builds the Go service and installs `cloudflared`
inside the image.

Build the image:

```bash
docker build -t tunnel-manager-backend .
```

Run the container with an explicit writable data mount and container-friendly
paths:

```bash
docker run --rm \
  -p 8080:8080 \
  --env-file .env \
  -e DB_PATH=/data/tunnel-manager.db \
  -e LOG_DIR=/data/logs \
  -e HTTP_ADDR=:8080 \
  -v "$(pwd)/data:/data" \
  tunnel-manager-backend
```

The image exposes port `8080`. If you deploy in a container, prefer absolute
paths such as `/data/...` instead of the relative host defaults from
`.env.example`.

## HTTP API

The service exposes these routes:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/domains` | Create a managed domain and its Cloudflare tunnel resources. |
| `GET` | `/api/domains` | List managed domains. Useful as a basic reachability check. |
| `GET` | `/api/domains/:id` | Fetch one managed domain. |
| `PUT` | `/api/domains/:id` | Update the origin URL for a managed domain. |
| `DELETE` | `/api/domains/:id` | Delete a managed domain and its Cloudflare resources. |
| `POST` | `/api/domains/:id/stop` | Stop the managed `cloudflared` process. |
| `POST` | `/api/domains/:id/restart` | Restart the managed `cloudflared` process. |
| `GET` | `/api/domains/:id/logs` | Return buffered log lines for a managed domain. |
| `GET` | `/api/domains/:id/metrics` | Proxy the managed domain's local Prometheus metrics endpoint. |

There is no dedicated `/healthz` endpoint in the current codebase. For a simple
HTTP-level check, use `GET /api/domains`.

## Operational Notes

- Service state is persisted in SQLite at `DB_PATH`.
- Tunnel tokens are stored encrypted and are not returned in the HTTP responses.
- The service creates `LOG_DIR` on startup if it does not already exist.
- Each managed `cloudflared` process gets a local metrics port from the
  configured range.
- On startup, invalid Cloudflare credentials, an invalid encryption key, an
  invalid metrics range, or an unsupported `CLOUDFLARED_PROTOCOL` will cause
  startup to fail.
- The container image includes `cloudflared`; host deployments must provide it.

## Repository Orientation

These paths matter most during deployment and operations:

- `cmd/server/main.go`: application entrypoint
- `migrations/`: Goose SQL migrations
- `internal/interfaces/http/`: API routes and handlers
- `internal/infrastructure/config/config.go`: environment loading and validation
- `Dockerfile`: container build and bundled `cloudflared`
- `Makefile`: run, build, test, and migration commands
