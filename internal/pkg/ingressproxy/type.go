package ingressproxy

import (
	"context"
	"tunnelmanager/internal/model"
)

type Proxy interface {
	URL() string
	Start() error
	Shutdown(ctx context.Context) error
	PrepareDomain(domainID, hostname string, oldRoutes, newRoutes []model.DomainRoute) error
	CommitDomain(domainID, hostname string, routes []model.DomainRoute) error
	RollbackDomain(domainID, hostname string, routes []model.DomainRoute) error
	RemoveDomain(domainID string)
}
