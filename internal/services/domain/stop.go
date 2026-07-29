package domainservice

import (
	"context"
	"errors"
	"os"
	"tunnelmanager/internal/pkg/constant"
)

func (s *domainService) StopDomain(ctx context.Context, id string) error {
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.sup.Stop(id); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	domain.Status = constant.StatusStopped
	return s.update(ctx, domain)
}
