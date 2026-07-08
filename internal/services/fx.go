package services

import (
	authservice "tunnelmanager/internal/services/auth"
	domainservice "tunnelmanager/internal/services/domain"

	"go.uber.org/fx"
)

var Module = fx.Module("services",
	domainservice.Module,
	authservice.Module,
)
