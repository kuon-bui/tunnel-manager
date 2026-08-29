package cloudflare

import (
	"context"
	"tunnelmanager/internal/model"
)

type TunnelInfo struct {
	TunnelID string
	Token    string
}

type IngressRule struct {
	Path    string
	Service string
}

type CloudflareClient interface {
	ListZones(ctx context.Context) ([]model.CloudflareZone, error)
	CreateTunnel(ctx context.Context, name string) (TunnelInfo, error)
	PutIngressConfig(ctx context.Context, tunnelID, hostname string, rules []IngressRule) error
	CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (dnsRecordID string, err error)
	DeleteDNSRecord(ctx context.Context, zoneID, dnsRecordID string) error
	DeleteTunnel(ctx context.Context, tunnelID string) error
}
