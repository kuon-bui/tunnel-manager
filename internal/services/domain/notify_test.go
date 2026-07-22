package domainservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"tunnelmanager/internal/model"
	domainrepo "tunnelmanager/internal/pkg/repo/domain"
)

func TestPublishNotifiesAndCoalesces(t *testing.T) {
	s := &domainService{subscribers: make(map[chan struct{}]struct{})}
	updates, cancel := s.Subscribe()
	defer cancel()

	s.publish()
	s.publish()

	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("missing notification")
	}
	select {
	case <-updates:
		t.Fatal("notifications did not coalesce")
	default:
	}
}

func TestPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	s := &domainService{subscribers: make(map[chan struct{}]struct{})}
	_, cancelSlow := s.Subscribe()
	defer cancelSlow()
	fast, cancelFast := s.Subscribe()
	defer cancelFast()
	s.publish()
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast subscriber missed notification")
	}

	done := make(chan struct{})
	go func() {
		s.publish()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked")
	}
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast subscriber missed second notification")
	}
}

func TestCancelSubscriptionIsIdempotent(t *testing.T) {
	s := &domainService{subscribers: make(map[chan struct{}]struct{})}
	updates, cancel := s.Subscribe()
	cancel()
	cancel()
	s.publish()
	select {
	case <-updates:
		t.Fatal("cancelled subscriber received notification")
	default:
	}
}

type notifyRepo struct {
	domainrepo.DomainRepository
	updateErr error
}

func (r *notifyRepo) Update(context.Context, *model.Domain) error {
	return r.updateErr
}

func TestUpdatePublishesOnlyAfterSuccessfulWrite(t *testing.T) {
	success := &domainService{
		repo:        &notifyRepo{},
		subscribers: make(map[chan struct{}]struct{}),
	}
	updates, cancel := success.Subscribe()
	defer cancel()
	if err := success.update(context.Background(), &model.Domain{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	default:
		t.Fatal("successful update did not publish")
	}

	wantErr := errors.New("write failed")
	failure := &domainService{
		repo:        &notifyRepo{updateErr: wantErr},
		subscribers: make(map[chan struct{}]struct{}),
	}
	failedUpdates, cancelFailed := failure.Subscribe()
	defer cancelFailed()
	if err := failure.update(context.Background(), &model.Domain{}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-failedUpdates:
		t.Fatal("failed update published")
	default:
	}
}
