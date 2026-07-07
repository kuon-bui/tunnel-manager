-- +goose Up
CREATE TABLE domains (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    origin_url TEXT NOT NULL,
    cloudflare_tunnel_id TEXT NOT NULL,
    dns_record_id TEXT NOT NULL,
    tunnel_token TEXT NOT NULL,
    status TEXT NOT NULL,
    metrics_port INTEGER NOT NULL,
    pid INTEGER NOT NULL DEFAULT 0,
    restart_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE domains;
