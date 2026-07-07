package domain

import (
	"context"
	"tunnelmanager/internal/model"
)

func (s *domainService) GetDomain(ctx context.Context, id string) (*model.Domain, error) {
	return s.repo.Get(ctx, id)
}
