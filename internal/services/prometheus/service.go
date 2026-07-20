package prometheusserivce

import (
	"context"
	"tunnelmanager/internal/pkg/config"
	domainrepo "tunnelmanager/internal/pkg/repo/domain"
	"tunnelmanager/internal/pkg/response"

	"go.uber.org/fx"
)

type PrometheusService interface {
	Discovery(ctx context.Context) ([]response.TargetsMetrics, error)
}

type prometheusService struct {
	domainRepo domainrepo.DomainRepository
	baseURL    string
}

type PrometheusServiceParams struct {
	fx.In
	Cfg        config.Config
	DomainRepo domainrepo.DomainRepository
}

func NewPrometheusService(params PrometheusServiceParams) PrometheusService {
	return &prometheusService{
		domainRepo: params.DomainRepo,
		baseURL:    "host.docker.internal:8180",
	}
}
