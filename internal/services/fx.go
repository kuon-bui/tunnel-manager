package services

import (
	authservice "tunnelmanager/internal/services/auth"
	domainservice "tunnelmanager/internal/services/domain"
	prometheusserivce "tunnelmanager/internal/services/prometheus"

	"go.uber.org/fx"
)

var Module = fx.Module("services",
	domainservice.Module,
	authservice.Module,
	prometheusserivce.Module,
)
