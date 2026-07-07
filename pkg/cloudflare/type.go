package cloudflare

import "context"

type TunnelInfo struct {
	TunnelID string
	Token    string
}

type CloudflareClient interface {
	CreateTunnel(ctx context.Context, name string) (TunnelInfo, error)
	PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL string) error
	CreateDNSRecord(ctx context.Context, hostname, tunnelID string) (dnsRecordID string, err error)
	DeleteDNSRecord(ctx context.Context, dnsRecordID string) error
	DeleteTunnel(ctx context.Context, tunnelID string) error
}
