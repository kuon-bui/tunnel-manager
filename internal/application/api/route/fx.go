package route

import (
	domainroute "tunnelmanager/internal/application/api/route/domain"

	"go.uber.org/fx"
)

var Module = fx.Module("route",
	domainroute.Module,
)
