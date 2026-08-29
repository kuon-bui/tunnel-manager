package domainservice

import (
	"context"
	"tunnelmanager/internal/model"
)

func (s *domainService) GetDomain(ctx context.Context, id string) (*model.Domain, error) {
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	domain.Routes, err = s.repo.ListRoutes(ctx, id)
	if err != nil {
		return nil, err
	}
	return domain, nil
}
