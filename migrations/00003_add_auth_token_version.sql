-- +goose Up
ALTER TABLE auths ADD COLUMN token_version INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE auths DROP COLUMN token_version;
