package process

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"
)

func NewSupervisor(cfg config.Config) ProcessSupervisor {
	return &Supervisor{
		binary:       cfg.CloudflaredBinary,
		protocol:     cfg.CloudflaredProtocol,
		eventHandler: nil,
		SleepFunc:    time.Sleep,
		StableAfter:  60 * time.Second,
		procs:        make(map[string]*managedProcess),
	}
}

func (s *Supervisor) SetEventHandler(handler func(ProcessEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventHandler = handler
}

func (s *Supervisor) Start(domainID, token string, metricsPort int, logWriter io.Writer) error {
	mp := &managedProcess{}
	s.mu.Lock()
	if _, exists := s.procs[domainID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("supervisor: process already running for %s", domainID)
	}
	s.procs[domainID] = mp
	s.mu.Unlock()

	cmd := s.buildCmd(token, metricsPort, logWriter)
	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		delete(s.procs, domainID)
		s.mu.Unlock()
		return fmt.Errorf("supervisor: start %s: %w", domainID, err)
	}

	mp.mu.Lock()
	stopRequested := mp.stopRequested
	if !stopRequested {
		mp.cmd = cmd
	}
	mp.mu.Unlock()

	if stopRequested {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		s.mu.Lock()
		delete(s.procs, domainID)
		s.mu.Unlock()
		s.emit(Event{DomainID: domainID, Status: model.StatusStopped})
		return nil
	}

	s.armStableTimer(mp)
	s.emit(Event{DomainID: domainID, PID: cmd.Process.Pid, Status: model.StatusActive})

	go s.supervise(domainID, token, metricsPort, logWriter, mp)
	return nil
}

func (s *Supervisor) Stop(domainID string) error {
	s.mu.Lock()
	mp, ok := s.procs[domainID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("supervisor: no running process for %s", domainID)
	}

	mp.mu.Lock()
	mp.stopRequested = true
	var proc *os.Process
	if mp.cmd != nil {
		proc = mp.cmd.Process
	}
	mp.mu.Unlock()

	if proc == nil {
		return nil
	}

	return proc.Signal(syscall.SIGTERM)
}

func (s *Supervisor) IsRunning(domainID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.procs[domainID]
	return ok
}
