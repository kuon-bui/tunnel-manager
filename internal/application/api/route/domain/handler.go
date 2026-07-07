package domainroute

import (
	domainservice "tunnelmanager/internal/services/domain"

	"go.uber.org/fx"
)

type DomainHandler struct {
	domainService domainservice.DomainService
}

type DomainHandlerParams struct {
	fx.In

	DomainService domainservice.DomainService
}

func NewDomainHandler(params DomainHandlerParams) *DomainHandler {
	return &DomainHandler{
		domainService: params.DomainService,
	}
}
