package domainroute

import (
	"tunnelmanager/pkg/common"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"domain-route",
	common.ProvideAsRoute(NewDomainRoute),
	fx.Provide(NewDomainHandler),
)
