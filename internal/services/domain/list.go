package domainservice

import (
	"context"
	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

func (s *domainService) ListDomains(ctx context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	return s.repo.List(ctx, req)
}
