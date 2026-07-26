package cloudflare

import (
	"context"
	"time"
)

type TunnelInfo struct {
	TunnelID string
	Token    string
}

type TunnelIngress struct {
	Hostname  string
	OriginURL string
	Path      string
}

type RemoteTunnel struct {
	TunnelID  string
	Name      string
	Status    string
	CreatedAt time.Time
	Ingress   []TunnelIngress
}

type CloudflareClient interface {
	CreateTunnel(ctx context.Context, name string) (TunnelInfo, error)
	ListRemoteTunnels(ctx context.Context) ([]RemoteTunnel, error)
	PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL, path string) error
	CreateDNSRecord(ctx context.Context, hostname, tunnelID string) (dnsRecordID string, err error)
	DeleteDNSRecord(ctx context.Context, dnsRecordID string) error
	DeleteTunnel(ctx context.Context, tunnelID string) error
}
