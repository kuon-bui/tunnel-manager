package domainservice

import (
	"context"
	"fmt"
	"time"
	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

func (s *domainService) UpdateOrigin(ctx context.Context, id, originURL string) (*model.Domain, error) {
	unlock := s.lockDomain(id)
	defer unlock()

	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	oldRoutes, err := s.repo.ListRoutes(ctx, id)
	if err != nil {
		return nil, err
	}
	inputs := routeInputs(oldRoutes)
	for i := range inputs {
		if inputs[i].Path == "/" {
			inputs[i].OriginURL = originURL
		}
	}
	routes, defaultOrigin, err := normalizeRoutes(id, inputs, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return s.replaceRoutesLocked(ctx, domain, oldRoutes, routes, defaultOrigin)
}

func (s *domainService) ReplaceRoutes(ctx context.Context, id string, inputs []domainrequest.RouteInput) (*model.Domain, error) {
	now := time.Now().UTC()
	routes, defaultOrigin, err := normalizeRoutes(id, inputs, now)
	if err != nil {
		return nil, err
	}

	unlock := s.lockDomain(id)
	defer unlock()

	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	oldRoutes, err := s.repo.ListRoutes(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.replaceRoutesLocked(ctx, domain, oldRoutes, routes, defaultOrigin)
}

func (s *domainService) replaceRoutesLocked(ctx context.Context, domain *model.Domain, oldRoutes, routes []model.DomainRoute, defaultOrigin string) (*model.Domain, error) {
	id := domain.ID
	if err := s.proxy.PrepareDomain(id, domain.Hostname, oldRoutes, routes); err != nil {
		return nil, fmt.Errorf("service: prepare ingress proxy: %w", err)
	}
	if err := s.cf.PutIngressConfig(ctx, domain.CloudflareTunnelID, domain.Hostname, s.cloudflareRules(routes)); err != nil {
		_ = s.proxy.RollbackDomain(id, domain.Hostname, oldRoutes)
		return nil, fmt.Errorf("%w: update ingress config", ErrCloudflareUnavailable)
	}
	if err := s.repo.ReplaceRoutes(ctx, id, defaultOrigin, routes); err != nil {
		rollbackErr := s.cf.PutIngressConfig(ctx, domain.CloudflareTunnelID, domain.Hostname, s.cloudflareRules(oldRoutes))
		if rollbackErr == nil {
			_ = s.proxy.RollbackDomain(id, domain.Hostname, oldRoutes)
		}
		return nil, fmt.Errorf("service: persist routes: %w", err)
	}
	if err := s.proxy.CommitDomain(id, domain.Hostname, routes); err != nil {
		return nil, fmt.Errorf("service: commit ingress proxy: %w", err)
	}
	s.publish()
	domain.OriginURL = defaultOrigin
	if len(routes) > 0 {
		domain.UpdatedAt = routes[0].UpdatedAt
	}
	domain.Routes = routes
	return domain, nil
}
