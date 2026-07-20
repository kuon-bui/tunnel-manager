package prometheusserivce

import (
	"context"
	"fmt"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/response"
)

func (s *prometheusService) Discovery(ctx context.Context) ([]response.TargetsMetrics, error) {
	domains, err := s.domainRepo.ListAll(ctx, constant.StatusActive) // Assuming you want to discover only active domains
	if err != nil {
		return nil, err
	}
	res := make([]response.TargetsMetrics, 0, len(domains))
	for _, domain := range domains {
		target := response.TargetsMetrics{
			Targets: []string{s.baseURL},
			Labels: response.LabelsMetrics{
				Env:         "pro",
				Domain:      domain.Hostname,
				MetricsPath: fmt.Sprintf("/api/domains/%s/metrics", domain.ID),
			},
		}

		res = append(res, target)
	}

	return res, nil
}
