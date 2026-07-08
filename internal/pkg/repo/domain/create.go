package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
)

func (r *domainRepository) Create(ctx context.Context, domain *model.Domain) error {
	_, err := r.db.NewInsert().Model(domain).Exec(ctx)
	return err
}
