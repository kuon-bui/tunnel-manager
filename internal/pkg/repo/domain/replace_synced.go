package domainrepo

import (
	"context"

	"github.com/uptrace/bun"

	"tunnelmanager/internal/model"
)

func (r *domainRepository) ReplaceSynced(ctx context.Context, domains []*model.Domain) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().
			Model((*model.Domain)(nil)).
			Where("managed = ?", false).
			Exec(ctx); err != nil {
			return err
		}

		for _, domain := range domains {
			if _, err := tx.NewInsert().
				Model(domain).
				On("CONFLICT (hostname) DO NOTHING").
				Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
