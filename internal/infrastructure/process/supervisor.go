package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"tunnelmanager/internal/application/ports"
	"tunnelmanager/internal/model"
)

const maxRestartAttempts = 5

type Event = ports.ProcessEvent

type managedProcess struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	stopRequested bool
	restartCount  int
	stableTimer   *time.Timer
}

type Supervisor struct {
	binary       string
	protocol     string
	eventHandler func(Event)

	SleepFunc   func(time.Duration)
	StableAfter time.Duration

	mu    sync.Mutex
	procs map[string]*managedProcess
}

func New(binary, protocol string, onEvent func(Event)) *Supervisor {
	return &Supervisor{
		binary:       binary,
		protocol:     protocol,
		eventHandler: onEvent,
		SleepFunc:    time.Sleep,
		StableAfter:  60 * time.Second,
		procs:        make(map[string]*managedProcess),
	}
}

func (s *Supervisor) SetEventHandler(handler func(ports.ProcessEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventHandler = handler
}

func (s *Supervisor) buildCmd(token string, metricsPort int, logWriter io.Writer) *exec.Cmd {
	cmd := exec.Command(
		s.binary,
		"tunnel",
		"--metrics",
		fmt.Sprintf("localhost:%d", metricsPort),
		"--protocol",
		s.protocol,
		"run",
		"--token",
		token,
	)
	fmt.Fprintf(logWriter, "cloudflared tunnel --metrics localhost:%d --protocol %s run --token token\n", metricsPort, s.protocol)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	return cmd
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

func (s *Supervisor) armStableTimer(mp *managedProcess) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.stableTimer != nil {
		mp.stableTimer.Stop()
	}
	mp.stableTimer = time.AfterFunc(s.StableAfter, func() {
		mp.mu.Lock()
		mp.restartCount = 0
		mp.mu.Unlock()
	})
}

func (s *Supervisor) supervise(domainID, token string, metricsPort int, logWriter io.Writer, mp *managedProcess) {
	for {
		waitErr := mp.cmd.Wait()

		mp.mu.Lock()
		if mp.stableTimer != nil {
			mp.stableTimer.Stop()
		}
		stopRequested := mp.stopRequested
		mp.mu.Unlock()

		if stopRequested {
			s.mu.Lock()
			delete(s.procs, domainID)
			s.mu.Unlock()
			s.emit(Event{DomainID: domainID, Status: model.StatusStopped})
			return
		}

		mp.mu.Lock()
		mp.restartCount++
		attempt := mp.restartCount
		mp.mu.Unlock()

		if attempt > maxRestartAttempts {
			s.mu.Lock()
			delete(s.procs, domainID)
			s.mu.Unlock()
			s.emit(Event{DomainID: domainID, Status: model.StatusError, RestartCount: maxRestartAttempts, Err: waitErr})
			return
		}

		backoff := time.Duration(1<<(attempt-1)) * time.Second
		s.SleepFunc(backoff)

		cmd := s.buildCmd(token, metricsPort, logWriter)
		if err := cmd.Start(); err != nil {
			continue
		}

		mp.mu.Lock()
		mp.cmd = cmd
		mp.mu.Unlock()
		s.armStableTimer(mp)
		s.emit(Event{DomainID: domainID, PID: cmd.Process.Pid, Status: model.StatusActive, RestartCount: attempt})
	}
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

func (s *Supervisor) emit(event Event) {
	s.mu.Lock()
	handler := s.eventHandler
	s.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}
