package cloudflare

import (
	"context"
	"fmt"

	cloudflareapi "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"

	"tunnelmanager/internal/application/ports"
)

type client struct {
	api       *cloudflareapi.Client
	accountID string
	zoneID    string
}

func New(apiToken, accountID, zoneID string) ports.CloudflareClient {
	return &client{
		api:       cloudflareapi.NewClient(option.WithAPIToken(apiToken)),
		accountID: accountID,
		zoneID:    zoneID,
	}
}

func (c *client) CreateTunnel(ctx context.Context, name string) (ports.TunnelInfo, error) {
	tunnel, err := c.api.ZeroTrust.Tunnels.Cloudflared.New(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID: cloudflareapi.F(c.accountID),
		Name:      cloudflareapi.F(name),
		ConfigSrc: cloudflareapi.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
	})
	if err != nil {
		return ports.TunnelInfo{}, fmt.Errorf("cloudflare: create tunnel: %w", err)
	}

	token, err := c.api.ZeroTrust.Tunnels.Cloudflared.Token.Get(ctx, tunnel.ID, zero_trust.TunnelCloudflaredTokenGetParams{
		AccountID: cloudflareapi.F(c.accountID),
	})
	if err != nil {
		return ports.TunnelInfo{}, fmt.Errorf("cloudflare: get tunnel token: %w", err)
	}

	return ports.TunnelInfo{TunnelID: tunnel.ID, Token: *token}, nil
}

func (c *client) PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL string) error {
	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tunnelID, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
		AccountID: cloudflareapi.F(c.accountID),
		Config: cloudflareapi.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
			Ingress: cloudflareapi.F([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
				{Hostname: cloudflareapi.F(hostname), Service: cloudflareapi.F(originURL)},
				{Service: cloudflareapi.F("http_status:404")},
			}),
		}),
	})
	if err != nil {
		return fmt.Errorf("cloudflare: put ingress config: %w", err)
	}
	return nil
}

func (c *client) CreateDNSRecord(ctx context.Context, hostname, tunnelID string) (string, error) {
	rec, err := c.api.DNS.Records.New(ctx, dns.RecordNewParams{
		ZoneID: cloudflareapi.F(c.zoneID),
		Body: dns.CNAMERecordParam{
			Name:    cloudflareapi.F(hostname),
			Type:    cloudflareapi.F(dns.CNAMERecordTypeCNAME),
			Content: cloudflareapi.F(tunnelID + ".cfargotunnel.com"),
			TTL:     cloudflareapi.F(dns.TTL(1)),
			Proxied: cloudflareapi.F(true),
		},
	})
	if err != nil {
		return "", fmt.Errorf("cloudflare: create dns record: %w", err)
	}
	return rec.ID, nil
}

func (c *client) DeleteDNSRecord(ctx context.Context, dnsRecordID string) error {
	_, err := c.api.DNS.Records.Delete(ctx, dnsRecordID, dns.RecordDeleteParams{
		ZoneID: cloudflareapi.F(c.zoneID),
	})
	if err != nil {
		return fmt.Errorf("cloudflare: delete dns record: %w", err)
	}
	return nil
}

func (c *client) DeleteTunnel(ctx context.Context, tunnelID string) error {
	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Delete(ctx, tunnelID, zero_trust.TunnelCloudflaredDeleteParams{
		AccountID: cloudflareapi.F(c.accountID),
	})
	if err != nil {
		return fmt.Errorf("cloudflare: delete tunnel: %w", err)
	}
	return nil
}
