-- +goose Up
CREATE TABLE domain_routes (
    id TEXT PRIMARY KEY,
    domain_id TEXT NOT NULL,
    path TEXT NOT NULL,
    origin_url TEXT NOT NULL,
    strip_prefix INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE,
    UNIQUE (domain_id, path)
);

CREATE INDEX idx_domain_routes_domain_path ON domain_routes(domain_id, path);

INSERT INTO domain_routes (
    id, domain_id, path, origin_url, strip_prefix, created_at, updated_at
)
SELECT
    'legacy-' || id, id, '/', origin_url, 0, created_at, updated_at
FROM domains;

-- +goose Down
DROP TABLE domain_routes;