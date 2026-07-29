package domainservice

import (
	"context"
	"sync"

	"tunnelmanager/internal/model"
)

func (s *domainService) Subscribe() (<-chan struct{}, func()) {
	updates := make(chan struct{}, 1)
	s.subscriberMu.Lock()
	s.subscribers[updates] = struct{}{}
	s.subscriberMu.Unlock()

	var once sync.Once
	return updates, func() {
		once.Do(func() {
			s.subscriberMu.Lock()
			delete(s.subscribers, updates)
			s.subscriberMu.Unlock()
		})
	}
}

func (s *domainService) publish() {
	// ponytail: Process-local fan-out supports one backend process; use shared pub/sub before adding replicas.
	s.subscriberMu.Lock()
	defer s.subscriberMu.Unlock()
	for updates := range s.subscribers {
		select {
		case updates <- struct{}{}:
		default:
		}
	}
}

func (s *domainService) create(ctx context.Context, domain *model.Domain) error {
	if err := s.repo.Create(ctx, domain); err != nil {
		return err
	}
	s.publish()
	return nil
}

func (s *domainService) update(ctx context.Context, domain *model.Domain) error {
	if err := s.repo.Update(ctx, domain); err != nil {
		return err
	}
	s.publish()
	return nil
}

func (s *domainService) updateBulk(ctx context.Context, domains []*model.Domain) error {
	if len(domains) == 0 {
		return nil
	}
	if err := s.repo.UpdateBulk(ctx, domains); err != nil {
		return err
	}
	s.publish()
	return nil
}

func (s *domainService) delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.publish()
	return nil
}
