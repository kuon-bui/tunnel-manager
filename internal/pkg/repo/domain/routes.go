package domainrepo

import (
	"context"
	"database/sql"

	"tunnelmanager/internal/model"

	"github.com/uptrace/bun"
)

func (r *domainRepository) ListRoutes(ctx context.Context, domainID string) ([]model.DomainRoute, error) {
	var routes []model.DomainRoute
	if err := r.db.NewSelect().Model(&routes).
		Where("domain_id = ?", domainID).
		OrderExpr("LENGTH(path) DESC").
		OrderExpr("path ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return routes, nil
}

func (r *domainRepository) ListRoutesByDomainIDs(ctx context.Context, domainIDs []string) (map[string][]model.DomainRoute, error) {
	result := make(map[string][]model.DomainRoute, len(domainIDs))
	if len(domainIDs) == 0 {
		return result, nil
	}

	var routes []model.DomainRoute
	if err := r.db.NewSelect().Model(&routes).
		Where("domain_id IN (?)", bun.In(domainIDs)).
		OrderExpr("domain_id ASC").
		OrderExpr("LENGTH(path) DESC").
		OrderExpr("path ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	for _, route := range routes {
		result[route.DomainID] = append(result[route.DomainID], route)
	}
	return result, nil
}

func (r *domainRepository) ReplaceRoutes(ctx context.Context, domainID, defaultOriginURL string, routes []model.DomainRoute) error {
	return r.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*model.Domain)(nil)).
			Set("origin_url = ?", defaultOriginURL).
			Set("updated_at = CURRENT_TIMESTAMP").
			Where("id = ?", domainID).
			Exec(ctx)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return model.ErrNotFound
		}

		if _, err := tx.NewDelete().Model((*model.DomainRoute)(nil)).Where("domain_id = ?", domainID).Exec(ctx); err != nil {
			return err
		}
		if len(routes) == 0 {
			return nil
		}
		_, err = tx.NewInsert().Model(&routes).Exec(ctx)
		return err
	})
}
