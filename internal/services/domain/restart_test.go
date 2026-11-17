package domainservice

import (
	"errors"
	"testing"
)

func TestRestartDomainStopsRunningProcessBeforeStarting(t *testing.T) {
	supervisor := &fakeProcessSupervisor{running: true}
	service, repo := newStartTestService(t, supervisor, stoppedDomain(t))

	if err := service.RestartDomain(t.Context(), "domain-1"); err != nil {
		t.Fatal(err)
	}
	if got := supervisor.calls; len(got) != 2 || got[0] != "stop" || got[1] != "start" {
		t.Fatalf("supervisor calls = %v, want [stop start]", got)
	}
	if got := repo.domains["domain-1"]; got.RestartCount != 0 || got.LastError != "" {
		t.Fatalf("restartCount/lastError = %d/%q", got.RestartCount, got.LastError)
	}
}

func TestRestartDomainStartsStoppedDomainWithoutStopping(t *testing.T) {
	supervisor := &fakeProcessSupervisor{}
	service, _ := newStartTestService(t, supervisor, stoppedDomain(t))

	if err := service.RestartDomain(t.Context(), "domain-1"); err != nil {
		t.Fatal(err)
	}
	if got := supervisor.calls; len(got) != 1 || got[0] != "start" {
		t.Fatalf("supervisor calls = %v, want [start]", got)
	}
}

func TestRestartDomainDoesNotStartWhenStopFails(t *testing.T) {
	stopErr := errors.New("stuck")
	supervisor := &fakeProcessSupervisor{running: true, stopErr: stopErr}
	service, _ := newStartTestService(t, supervisor, stoppedDomain(t))

	if err := service.RestartDomain(t.Context(), "domain-1"); !errors.Is(err, stopErr) {
		t.Fatalf("err = %v", err)
	}
	if supervisor.startCalls != 0 {
		t.Fatalf("process starts = %d", supervisor.startCalls)
	}
}
