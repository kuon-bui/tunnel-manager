package portalloc

import (
	"fmt"
	"tunnelmanager/pkg/config"
)

type Allocator struct {
	start, end int
}

func NewPortAllocator(cfg config.Config) *Allocator {
	return &Allocator{start: cfg.MetricsPortRangeStart, end: cfg.MetricsPortRangeEnd}
}

func (a *Allocator) Allocate(taken map[int]bool) (int, error) {
	for port := a.start; port <= a.end; port++ {
		if !taken[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("portalloc: no free port in range %d-%d", a.start, a.end)
}
