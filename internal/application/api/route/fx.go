package route

import (
	authroute "tunnelmanager/internal/application/api/route/auth"
	domainroute "tunnelmanager/internal/application/api/route/domain"

	"go.uber.org/fx"
)

var Module = fx.Module("route",
	domainroute.Module,
	authroute.Module,
)
