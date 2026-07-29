package domainservice

import (
	"context"
	"fmt"
	"strings"

	"tunnelmanager/internal/model"
)

func (s *domainService) ListCloudflareZones(ctx context.Context) ([]model.CloudflareZone, error) {
	zones, err := s.cf.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: list zones: %w", ErrCloudflareUnavailable, err)
	}
	return zones, nil
}

func validateZoneHostname(zones []model.CloudflareZone, zoneID, hostname string) (string, error) {
	normalizedHostname := normalizeDNSName(hostname)
	for _, zone := range zones {
		if zone.ID != zoneID || zone.Status != "active" {
			continue
		}
		zoneName := normalizeDNSName(zone.Name)
		if normalizedHostname == zoneName || strings.HasSuffix(normalizedHostname, "."+zoneName) {
			return normalizedHostname, nil
		}
		break
	}
	return "", ErrInvalidZone
}

func normalizeDNSName(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
