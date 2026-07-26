package cloudflare

import (
	"context"
	"fmt"

	cloudflareapi "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"

	"tunnelmanager/internal/pkg/config"
)

type client struct {
	api       *cloudflareapi.Client
	accountID string
	zoneID    string
}

func NewCloudflareClient(cfg config.Config) CloudflareClient {
	return &client{
		api:       cloudflareapi.NewClient(option.WithAPIToken(cfg.CloudflareAPIToken)),
		accountID: cfg.CloudflareAccountID,
		zoneID:    cfg.CloudflareZoneID,
	}
}

func (c *client) CreateTunnel(ctx context.Context, name string) (TunnelInfo, error) {
	tunnel, err := c.api.ZeroTrust.Tunnels.Cloudflared.New(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID: cloudflareapi.F(c.accountID),
		Name:      cloudflareapi.F(name),
		ConfigSrc: cloudflareapi.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
	})
	if err != nil {
		return TunnelInfo{}, fmt.Errorf("cloudflare: create tunnel: %w", err)
	}

	token, err := c.api.ZeroTrust.Tunnels.Cloudflared.Token.Get(ctx, tunnel.ID, zero_trust.TunnelCloudflaredTokenGetParams{
		AccountID: cloudflareapi.F(c.accountID),
	})
	if err != nil {
		return TunnelInfo{}, fmt.Errorf("cloudflare: get tunnel token: %w", err)
	}

	return TunnelInfo{TunnelID: tunnel.ID, Token: *token}, nil
}

func (c *client) ListRemoteTunnels(ctx context.Context) ([]RemoteTunnel, error) {
	pager := c.api.ZeroTrust.Tunnels.Cloudflared.ListAutoPaging(ctx, zero_trust.TunnelCloudflaredListParams{
		AccountID: cloudflareapi.F(c.accountID),
		IsDeleted: cloudflareapi.F(false),
		PerPage:   cloudflareapi.F(100.0),
	})

	tunnels := make([]RemoteTunnel, 0)
	for pager.Next() {
		tunnel := pager.Current()
		if tunnel.ConfigSrc != "cloudflare" {
			continue
		}

		configuration, err := c.api.ZeroTrust.Tunnels.Cloudflared.Configurations.Get(
			ctx,
			tunnel.ID,
			zero_trust.TunnelCloudflaredConfigurationGetParams{
				AccountID: cloudflareapi.F(c.accountID),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("cloudflare: get tunnel %s configuration: %w", tunnel.ID, err)
		}

		remote := RemoteTunnel{
			TunnelID:  tunnel.ID,
			Name:      tunnel.Name,
			Status:    string(tunnel.Status),
			CreatedAt: tunnel.CreatedAt,
			Ingress:   make([]TunnelIngress, 0, len(configuration.Config.Ingress)),
		}
		for _, ingress := range configuration.Config.Ingress {
			if ingress.Hostname == "" {
				continue
			}
			remote.Ingress = append(remote.Ingress, TunnelIngress{
				Hostname:  ingress.Hostname,
				OriginURL: ingress.Service,
				Path:      ingress.Path,
			})
		}
		tunnels = append(tunnels, remote)
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("cloudflare: list tunnels: %w", err)
	}

	return tunnels, nil
}

func (c *client) PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL, path string) error {
	ingress := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
		Hostname: cloudflareapi.F(hostname),
		Service:  cloudflareapi.F(originURL),
	}
	if path != "" {
		ingress.Path = cloudflareapi.F(path)
	}

	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tunnelID, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
		AccountID: cloudflareapi.F(c.accountID),
		Config: cloudflareapi.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
			Ingress: cloudflareapi.F([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
				ingress,
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
