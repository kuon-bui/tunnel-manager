package domainservice

import (
	"context"
	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

func (s *domainService) ListDomains(ctx context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	domains, cursor, err := s.repo.List(ctx, req)
	if err != nil || len(domains) == 0 {
		return domains, cursor, err
	}
	ids := make([]string, 0, len(domains))
	for _, domain := range domains {
		ids = append(ids, domain.ID)
	}
	routes, err := s.repo.ListRoutesByDomainIDs(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	for _, domain := range domains {
		domain.Routes = routes[domain.ID]
	}
	return domains, cursor, nil
}
