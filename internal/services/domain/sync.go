package domainservice

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/constant"
)

func (s *domainService) SyncCloudflare(ctx context.Context) error {
	tunnels, err := s.cf.ListRemoteTunnels(ctx)
	if err != nil {
		return fmt.Errorf("service: sync Cloudflare tunnels: %w", err)
	}

	existing, err := s.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("service: list domains before Cloudflare sync: %w", err)
	}

	managedTunnelIDs := make(map[string]struct{}, len(existing))
	for _, domain := range existing {
		if domain.Managed {
			managedTunnelIDs[domain.CloudflareTunnelID] = struct{}{}
		}
	}

	now := time.Now().UTC()
	synced := make([]*model.Domain, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if _, managed := managedTunnelIDs[tunnel.TunnelID]; managed {
			continue
		}
		for _, ingress := range tunnel.Ingress {
			if ingress.Hostname == "" {
				continue
			}

			createdAt := tunnel.CreatedAt
			if createdAt.IsZero() {
				createdAt = now
			}
			synced = append(synced, &model.Domain{
				ID:                   syncedDomainID(tunnel.TunnelID, ingress.Hostname),
				Hostname:             ingress.Hostname,
				OriginURL:            ingress.OriginURL,
				Path:                 ingress.Path,
				CloudflareTunnelID:   tunnel.TunnelID,
				CloudflareTunnelName: tunnel.Name,
				CloudflareStatus:     tunnel.Status,
				DNSRecordID:          "",
				EncryptedTunnelToken: "",
				Managed:              false,
				Status:               mapCloudflareStatus(tunnel.Status),
				MetricsPort:          0,
				CreatedAt:            createdAt,
				UpdatedAt:            now,
			})
		}
	}

	if err := s.repo.ReplaceSynced(ctx, synced); err != nil {
		return fmt.Errorf("service: replace synced Cloudflare domains: %w", err)
	}
	return nil
}

func syncedDomainID(tunnelID, hostname string) string {
	return uuid.NewSHA1(
		uuid.NameSpaceURL,
		[]byte("cloudflare-tunnel:"+tunnelID+":"+hostname),
	).String()
}

func mapCloudflareStatus(status string) constant.DomainStatus {
	switch status {
	case "healthy":
		return constant.StatusActive
	case "inactive":
		return constant.StatusStopped
	case "degraded", "down":
		return constant.StatusError
	default:
		return constant.StatusPending
	}
}
