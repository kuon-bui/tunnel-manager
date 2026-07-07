package domainservice

import (
	"context"
	"tunnelmanager/internal/model"
)

func (s *domainService) ListDomains(ctx context.Context) ([]model.Domain, error) {
	return s.repo.List(ctx)
}
