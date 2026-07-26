-- +goose Up
ALTER TABLE domains ADD COLUMN cloudflare_tunnel_name TEXT NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN cloudflare_status TEXT NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN managed INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE domains DROP COLUMN managed;
ALTER TABLE domains DROP COLUMN cloudflare_status;
ALTER TABLE domains DROP COLUMN cloudflare_tunnel_name;
