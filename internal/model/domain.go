package model

import (
	"errors"
	"time"
	"tunnelmanager/internal/pkg/constant"

	"github.com/uptrace/bun"
)

var ErrNotFound = errors.New("model: not found")
var ErrSyncedDomainReadOnly = errors.New("model: synced Cloudflare domain is read-only")

type Domain struct {
	bun.BaseModel `bun:"table:domains,alias:d"`

	ID                   string                `bun:"id,pk" json:"id"`
	Hostname             string                `bun:"hostname,notnull,unique" json:"hostname"`
	OriginURL            string                `bun:"origin_url,notnull" json:"originUrl"`
	Path                 string                `bun:"path,notnull" json:"path"`
	CloudflareTunnelID   string                `bun:"cloudflare_tunnel_id,notnull" json:"cloudflareTunnelId"`
	CloudflareTunnelName string                `bun:"cloudflare_tunnel_name,notnull" json:"cloudflareTunnelName"`
	CloudflareStatus     string                `bun:"cloudflare_status,notnull" json:"cloudflareStatus"`
	DNSRecordID          string                `bun:"dns_record_id,notnull" json:"dnsRecordId"`
	EncryptedTunnelToken string                `bun:"tunnel_token,notnull" json:"-"`
	Managed              bool                  `bun:"managed,notnull" json:"managed"`
	Status               constant.DomainStatus `bun:"status,notnull" json:"status"`
	MetricsPort          int                   `bun:"metrics_port,notnull" json:"metricsPort"`
	PID                  int                   `bun:"pid" json:"pid"`
	RestartCount         int                   `bun:"restart_count,notnull,default:0" json:"restartCount"`
	LastError            string                `bun:"last_error" json:"lastError"`
	CreatedAt            time.Time             `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt            time.Time             `bun:"updated_at,notnull" json:"updatedAt"`
}
