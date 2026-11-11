# Production-like Docker Compose Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the Tunnel Manager backend and frontend as production-built containers from `/home/kuon/code/docker-compose.yml`.

**Architecture:** Compose builds both sibling repositories, exposes only the Next.js frontend, and connects it to the Go backend through a private network. The backend applies embedded migration files before startup and persists SQLite plus logs in a named volume.

**Tech Stack:** Docker Compose, multi-stage Docker builds, Go, Goose, cloudflared, Next.js 16, pnpm, Alpine Linux

## Global Constraints

- Compose lives at `/home/kuon/code/docker-compose.yml`.
- Frontend is published on host port 3000; backend remains private.
- Backend credentials come from `tunnel-manager/.env` and are not copied into images.
- Backend data persists at `/data`.
- Frontend uses `API_BASE_URL=http://backend:8080`.
- Migrations must succeed before the backend server starts.

---

### Task 1: Production Backend Container

**Files:**
- Modify: `/home/kuon/code/tunnel-manager/Dockerfile`
- Create: `/home/kuon/code/tunnel-manager/docker-entrypoint.sh`
- Modify: `/home/kuon/code/tunnel-manager/.dockerignore`

**Interfaces:**
- Consumes: migration SQL under `/home/kuon/code/tunnel-manager/migrations`
- Produces: an image that accepts existing environment variables, stores state under `/data`, exposes port 8080, and starts through `/usr/local/bin/docker-entrypoint.sh`

- [ ] **Step 1: Add a container contract test script**

Create a temporary shell assertion that builds the image, inspects its entrypoint, and verifies that migrations and the Goose binary exist in it.

- [ ] **Step 2: Run the assertion before changes**

Run: `docker build -t tunnel-manager-backend:compose-test . && docker run --rm --entrypoint sh tunnel-manager-backend:compose-test -c 'test -x /usr/local/bin/goose && test -f /app/migrations/00001_create_domains.sql && test -x /usr/local/bin/docker-entrypoint.sh'`

Expected: FAIL because the current image does not contain Goose, migrations, or the entrypoint script.

- [ ] **Step 3: Implement the production image and startup script**

Install Goose in the build stage, copy it plus migration files into the runtime image, download the cloudflared binary for `TARGETARCH`, create `/data/logs`, and run as a dedicated non-root user. The entrypoint must execute:

```sh
GOOSE_DRIVER=sqlite3 \
GOOSE_DBSTRING="$DB_PATH" \
GOOSE_MIGRATION_DIR=/app/migrations \
GOOSE_TABLE=migrations \
/usr/local/bin/goose up -allow-missing
exec /usr/local/bin/tunnel-manager
```

- [ ] **Step 4: Re-run the image contract and backend verification**

Run: `go test ./... && go build ./... && docker build -t tunnel-manager-backend:compose-test . && docker run --rm --entrypoint sh tunnel-manager-backend:compose-test -c 'test -x /usr/local/bin/goose && test -f /app/migrations/00001_create_domains.sql && test -x /usr/local/bin/docker-entrypoint.sh'`

Expected: all Go tests/builds pass and the container assertion exits 0.

### Task 2: Production Frontend Container

**Files:**
- Create: `/home/kuon/code/tunnel-manager-fe/Dockerfile`
- Create: `/home/kuon/code/tunnel-manager-fe/.dockerignore`
- Modify: `/home/kuon/code/tunnel-manager-fe/next.config.ts`

**Interfaces:**
- Consumes: `API_BASE_URL` at container runtime
- Produces: a minimal standalone Next.js image listening on `0.0.0.0:3000`

- [ ] **Step 1: Verify the image is initially unavailable**

Run: `docker build -t tunnel-manager-frontend:compose-test .`

Expected: FAIL because no frontend Dockerfile exists.

- [ ] **Step 2: Enable standalone output and add the multi-stage image**

Set `output: "standalone"` in `next.config.ts`. Use Corepack with the lockfile for dependencies, run `pnpm build`, then copy `.next/standalone`, `.next/static`, and `public` into a non-root Node Alpine runtime. Set `HOSTNAME=0.0.0.0`, `PORT=3000`, and run `node server.js`.

- [ ] **Step 3: Verify frontend source and image**

Run: `pnpm test && pnpm lint && pnpm build && docker build -t tunnel-manager-frontend:compose-test .`

Expected: tests, lint, Next.js build, and image build all pass.

### Task 3: Compose Orchestration and Operator Documentation

**Files:**
- Modify: `/home/kuon/code/docker-compose.yml`
- Modify: `/home/kuon/code/tunnel-manager/README.md`
- Modify: `/home/kuon/code/tunnel-manager/.env.example`

**Interfaces:**
- Consumes: `./tunnel-manager/.env`, backend/frontend Dockerfiles, named volume `tunnel-manager-data`
- Produces: `docker compose up -d --build` deployment at `http://localhost:3000`

- [ ] **Step 1: Add the Compose definition**

Define `backend` and `frontend` services. Backend loads `./tunnel-manager/.env`, overrides paths for `/data`, mounts `tunnel-manager-data:/data`, includes a healthcheck against `POST /api/auth/logout`, and uses `restart: unless-stopped`. Frontend sets `API_BASE_URL=http://backend:8080`, publishes `3000:3000`, waits for backend health, includes an HTTP healthcheck, and uses the same restart policy.

- [ ] **Step 2: Document exact operator workflow**

Update `.env.example` comments and README with copying the env file, generating `ENCRYPTION_KEY` and `JWT_SECRET`, setting Cloudflare/admin values, and these commands:

```sh
cd /home/kuon/code
docker compose config
docker compose up -d --build
docker compose ps
docker compose logs -f
docker compose down
```

- [ ] **Step 3: Validate Compose without exposing secrets**

Run: `docker compose config --quiet`

Expected: exit 0. Do not print the fully rendered configuration because it contains values loaded from `.env`.

- [ ] **Step 4: Run the integrated smoke test**

Run: `docker compose up -d --build`, `docker compose ps`, and `curl -fsS http://localhost:3000/` (following redirects as needed).

Expected: backend and frontend report healthy, and the frontend returns an HTTP response.

- [ ] **Step 5: Review the working tree**

Run `git diff --check` and inspect `git status --short` in both repositories. Ensure no `.env`, database, log, or generated build artifact is staged.
