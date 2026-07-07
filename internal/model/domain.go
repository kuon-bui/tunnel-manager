package model

import (
	"errors"
	"time"

	"github.com/uptrace/bun"
)

var ErrNotFound = errors.New("model: not found")

type Status string

const (
	StatusPending Status = "pending"
	StatusActive  Status = "active"
	StatusError   Status = "error"
	StatusStopped Status = "stopped"
)

type Domain struct {
	bun.BaseModel `bun:"table:domains,alias:d"`

	ID                   string    `bun:"id,pk"`
	Hostname             string    `bun:"hostname,notnull,unique"`
	OriginURL            string    `bun:"origin_url,notnull"`
	CloudflareTunnelID   string    `bun:"cloudflare_tunnel_id,notnull"`
	DNSRecordID          string    `bun:"dns_record_id,notnull"`
	EncryptedTunnelToken string    `bun:"tunnel_token,notnull"`
	Status               Status    `bun:"status,notnull"`
	MetricsPort          int       `bun:"metrics_port,notnull"`
	PID                  int       `bun:"pid"`
	RestartCount         int       `bun:"restart_count,notnull,default:0"`
	LastError            string    `bun:"last_error"`
	CreatedAt            time.Time `bun:"created_at,notnull"`
	UpdatedAt            time.Time `bun:"updated_at,notnull"`
}
