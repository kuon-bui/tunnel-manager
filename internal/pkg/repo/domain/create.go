package domainrepo

import (
	"context"
	"database/sql"
	"tunnelmanager/internal/model"

	"github.com/uptrace/bun"
)

func (r *domainRepository) Create(ctx context.Context, domain *model.Domain) error {
	return r.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(domain).Exec(ctx); err != nil {
			return err
		}
		if len(domain.Routes) == 0 {
			return nil
		}
		_, err := tx.NewInsert().Model(&domain.Routes).Exec(ctx)
		return err
	})
}
