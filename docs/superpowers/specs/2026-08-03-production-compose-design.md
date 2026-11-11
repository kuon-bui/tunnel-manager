# Production-like Docker Compose Design

## Goal

Run the Tunnel Manager backend and frontend together from a Compose project
located at `/home/kuon/code/docker-compose.yml`. The deployment should resemble a
small production installation while remaining convenient to run locally.

## Architecture

Compose builds the backend from `./tunnel-manager` and the frontend from
`./tunnel-manager-fe`. The frontend is the only published application service,
on host port 3000. It proxies same-origin `/api` requests to the backend through
the private Compose network using `http://backend:8080`.

The backend stores its SQLite database and cloudflared logs in persistent Docker
storage mounted at `/data`. Backend secrets and Cloudflare credentials come from
`tunnel-manager/.env`; no real credentials are embedded in Compose or images.

## Images and Startup

The frontend uses a multi-stage production image: dependencies are installed
from the lockfile, Next.js is built, and the final stage runs `next start` as a
non-root user.

The backend image contains the compiled server, `cloudflared`, migration files,
and a migration runner. Its startup command applies pending SQLite migrations
before launching the server. Migration failure stops the container instead of
starting against an incompatible schema.

The frontend waits for the backend healthcheck before starting. Both services
use restart policies suitable for a long-running local or single-host install.

## Configuration

Compose overrides container-specific backend paths and addresses:

- `DB_PATH=/data/tunnel-manager.db`
- `LOG_DIR=/data/logs`
- `HTTP_ADDR=:8080`
- `CORS_ALLOWED_ORIGIN=http://localhost:3000`
- `API_BASE_URL=http://backend:8080` for the frontend

All other backend values are loaded from `tunnel-manager/.env`. The Compose
configuration validates required secrets through the application's existing
startup validation.

## Networking and Exposure

Only frontend port `3000` is published. Backend port `8080` remains private to
the Compose network because normal browser traffic passes through the Next.js
API proxy. Managed `cloudflared` subprocesses run in the backend container and
use its configured internal metrics port range.

## Health and Failure Handling

The backend healthcheck uses an unauthenticated API endpoint to verify that the
HTTP server responds. The frontend healthcheck verifies its root HTTP service.
Containers restart unless explicitly stopped. A failed migration or invalid
configuration leaves the backend unhealthy/stopped and prevents the frontend
from being treated as ready.

## Verification

Validate the implementation with backend tests/build, frontend tests/lint/build,
`docker compose config`, image builds, and a startup smoke test when Docker is
available. Documentation will include setup, secret generation, startup,
status, logs, and shutdown commands.
