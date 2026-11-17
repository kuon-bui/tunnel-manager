package process

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
)

// killWaitTimeout bounds how long StopAndWait waits for supervision to end
// after the process has been force-killed.
const killWaitTimeout = 5 * time.Second

func NewSupervisor(cfg config.Config) ProcessSupervisor {
	return &Supervisor{
		binary:       cfg.CloudflaredBinary,
		protocol:     cfg.CloudflaredProtocol,
		eventHandler: nil,
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
	mp := newManagedProcess()
	s.mu.Lock()
	if _, exists := s.procs[domainID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("supervisor: process already running for %s", domainID)
	}
	s.procs[domainID] = mp
	s.mu.Unlock()

	cmd := s.buildCmd(token, metricsPort, logWriter)
	if err := cmd.Start(); err != nil {
		s.finish(domainID, mp, nil)
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
		s.finish(domainID, mp, &Event{DomainID: domainID, Status: constant.StatusStopped})
		return nil
	}

	s.armStableTimer(mp)
	s.emit(Event{DomainID: domainID, PID: cmd.Process.Pid, Status: constant.StatusActive})

	go s.supervise(domainID, token, metricsPort, logWriter, mp)
	return nil
}

func (s *Supervisor) Stop(domainID string) error {
	mp, ok := s.lookup(domainID)
	if !ok {
		return fmt.Errorf("supervisor: no running process for %s", domainID)
	}
	proc := s.requestStop(mp)
	if proc == nil {
		return nil
	}
	return proc.Signal(syscall.SIGTERM)
}

func (s *Supervisor) StopAndWait(domainID string, grace time.Duration) error {
	mp, ok := s.lookup(domainID)
	if !ok {
		return nil
	}
	if proc := s.requestStop(mp); proc != nil {
		if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			// SIGTERM is unsupported on some platforms (e.g. Windows); fall back to kill.
			_ = proc.Kill()
		}
	}

	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case <-mp.done:
		return nil
	case <-graceTimer.C:
	}

	if proc := mp.process(); proc != nil {
		_ = proc.Kill()
	}
	killTimer := time.NewTimer(killWaitTimeout)
	defer killTimer.Stop()
	select {
	case <-mp.done:
		return nil
	case <-killTimer.C:
		return fmt.Errorf("supervisor: process for %s did not exit after kill", domainID)
	}
}

func (s *Supervisor) IsRunning(domainID string) bool {
	_, ok := s.lookup(domainID)
	return ok
}

func (s *Supervisor) lookup(domainID string) (*managedProcess, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mp, ok := s.procs[domainID]
	return mp, ok
}

// requestStop marks the process as stopping, interrupts any pending restart
// backoff and returns the current OS process, if any.
func (s *Supervisor) requestStop(mp *managedProcess) *os.Process {
	mp.mu.Lock()
	mp.stopRequested = true
	mp.mu.Unlock()
	mp.stopOnce.Do(func() { close(mp.stopCh) })
	return mp.process()
}

func (mp *managedProcess) process() *os.Process {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.cmd == nil {
		return nil
	}
	return mp.cmd.Process
}

// finish ends supervision of mp. The final event is emitted before the
// process is unregistered and waiters are released, so a caller that starts a
// replacement after StopAndWait never has its "active" status overwritten by
// a late "stopped" event from the old process.
func (s *Supervisor) finish(domainID string, mp *managedProcess, ev *Event) {
	if ev != nil {
		s.emit(*ev)
	}
	s.mu.Lock()
	if s.procs[domainID] == mp {
		delete(s.procs, domainID)
	}
	s.mu.Unlock()
	close(mp.done)
}
