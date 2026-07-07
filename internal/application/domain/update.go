package domain

import (
	"context"
	"fmt"
	"tunnelmanager/internal/model"
)

func (s *domainService) UpdateOrigin(ctx context.Context, id, originURL string) (*model.Domain, error) {
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.cf.PutIngressConfig(ctx, domain.CloudflareTunnelID, domain.Hostname, originURL); err != nil {
		return nil, fmt.Errorf("service: update ingress config: %w", err)
	}
	domain.OriginURL = originURL
	if err := s.repo.Update(ctx, domain); err != nil {
		return nil, err
	}
	return domain, nil
}
