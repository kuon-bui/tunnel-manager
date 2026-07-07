package services

import (
	domainservice "tunnelmanager/internal/services/domain"

	"go.uber.org/fx"
)

var Module = fx.Module("services",
	domainservice.Module,
)
