package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/repo/scopes"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/uptrace/bun"
)

func (r *domainRepository) List(ctx context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	var domains []*model.Domain
	q := r.db.NewSelect().Model(&domains)

	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}

	if req.Hostname != "" {
		q = q.Where("hostname LIKE ?", "%"+req.Hostname+"%")
	}

	q = q.Apply(scopes.Paginate(req.Pagination))

	if err := q.Order("created_at ASC", "id ASC").Scan(ctx); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if req.PageSize > 0 && len(domains) == req.PageSize {
		last := domains[len(domains)-1]
		cursor, err := scopes.EncodeCursor(last.ID, last.CreatedAt)
		if err != nil {
			return nil, "", err
		}
		nextCursor = cursor
	}

	return domains, nextCursor, nil
}

func (r *domainRepository) ListAll(ctx context.Context, statuses ...constant.DomainStatus) ([]*model.Domain, error) {
	var domains []*model.Domain
	q := r.db.NewSelect().Model(&domains).Order("created_at ASC", "id ASC")
	if len(statuses) > 0 {
		q = q.Where("status IN (?)", bun.In(statuses))
	}

	if err := q.Scan(ctx); err != nil {
		return nil, err
	}

	return domains, nil
}

func (r *domainRepository) ListTakenPorts(ctx context.Context) (map[int]bool, error) {
	var ports []int
	if err := r.db.NewSelect().Model((*model.Domain)(nil)).Column("metrics_port").Scan(ctx, &ports); err != nil {
		return nil, err
	}

	taken := make(map[int]bool, len(ports))
	for _, p := range ports {
		taken[p] = true
	}
	return taken, nil
}
