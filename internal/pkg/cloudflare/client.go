package cloudflare

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"tunnelmanager/internal/model"

	cloudflareapi "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
	"github.com/cloudflare/cloudflare-go/v6/zones"

	"tunnelmanager/internal/pkg/config"
)

type client struct {
	api       *cloudflareapi.Client
	accountID string
}

func NewCloudflareClient(cfg config.Config) CloudflareClient {
	return &client{
		api:       cloudflareapi.NewClient(option.WithAPIToken(cfg.CloudflareAPIToken)),
		accountID: cfg.CloudflareAccountID,
	}
}

func (c *client) ListZones(ctx context.Context) ([]model.CloudflareZone, error) {
	pager := c.api.Zones.ListAutoPaging(ctx, zones.ZoneListParams{
		Account: cloudflareapi.F(zones.ZoneListParamsAccount{
			ID: cloudflareapi.F(c.accountID),
		}),
		Page:    cloudflareapi.F(float64(1)),
		PerPage: cloudflareapi.F(float64(50)),
		Status:  cloudflareapi.F(zones.ZoneListParamsStatusActive),
	})

	result := make([]model.CloudflareZone, 0)
	for pager.Next() {
		zone := pager.Current()
		if zone.Status != zones.ZoneStatusActive {
			continue
		}
		result = append(result, model.CloudflareZone{
			ID:     zone.ID,
			Name:   strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone.Name)), "."),
			Status: string(zone.Status),
		})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("cloudflare: list zones: %w", err)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
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

func (c *client) PutIngressConfig(ctx context.Context, tunnelID, hostname string, rules []IngressRule) error {
	if len(rules) == 0 {
		return fmt.Errorf("cloudflare: put ingress config: at least one application rule is required")
	}
	ingress := make([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress, 0, len(rules)+1)
	for _, rule := range rules {
		item := zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
			Hostname: cloudflareapi.F(hostname),
			Service:  cloudflareapi.F(rule.Service),
		}
		if rule.Path != "" {
			item.Path = cloudflareapi.F(rule.Path)
		}
		ingress = append(ingress, item)
	}
	ingress = append(ingress, zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
		Service: cloudflareapi.F("http_status:404"),
	})

	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tunnelID, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
		AccountID: cloudflareapi.F(c.accountID),
		Config: cloudflareapi.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
			Ingress: cloudflareapi.F(ingress),
		}),
	})
	if err != nil {
		return fmt.Errorf("cloudflare: put ingress config: %w", err)
	}
	return nil
}

func (c *client) CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (string, error) {
	rec, err := c.api.DNS.Records.New(ctx, dns.RecordNewParams{
		ZoneID: cloudflareapi.F(zoneID),
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

func (c *client) DeleteDNSRecord(ctx context.Context, zoneID, dnsRecordID string) error {
	_, err := c.api.DNS.Records.Delete(ctx, dnsRecordID, dns.RecordDeleteParams{
		ZoneID: cloudflareapi.F(zoneID),
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
