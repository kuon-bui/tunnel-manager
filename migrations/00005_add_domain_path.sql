-- +goose Up
ALTER TABLE domains ADD COLUMN path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE domains DROP COLUMN path;
