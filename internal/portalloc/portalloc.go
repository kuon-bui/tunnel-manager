package portalloc

import "fmt"

type Allocator struct {
	start, end int
}

func NewAllocator(start, end int) *Allocator {
	return &Allocator{start: start, end: end}
}

func (a *Allocator) Allocate(taken map[int]bool) (int, error) {
	for port := a.start; port <= a.end; port++ {
		if !taken[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("portalloc: no free port in range %d-%d", a.start, a.end)
}
