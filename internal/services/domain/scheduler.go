package domainservice

import (
	"context"
	"log"
	"sync"
	"time"
)

type cloudflareSyncScheduler struct {
	interval time.Duration
	sync     func(context.Context) error

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newCloudflareSyncScheduler(
	interval time.Duration,
	syncFunc func(context.Context) error,
) *cloudflareSyncScheduler {
	return &cloudflareSyncScheduler{
		interval: interval,
		sync:     syncFunc,
	}
}

func (s *cloudflareSyncScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done

	go func() {
		defer close(done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				syncCtx, cancel := context.WithTimeout(ctx, s.interval)
				if err := s.sync(syncCtx); err != nil {
					log.Printf("periodic Cloudflare tunnel sync: %v", err)
				}
				cancel()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *cloudflareSyncScheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
