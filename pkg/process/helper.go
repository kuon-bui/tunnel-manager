package process

import (
	"fmt"
	"io"
	"os/exec"
	"time"
	"tunnelmanager/internal/model"
)

// buildCmd constructs the command to run cloudflared with the given parameters.
func (s *Supervisor) buildCmd(token string, metricsPort int, logWriter io.Writer) *exec.Cmd {
	cmd := exec.Command(
		s.binary,
		"tunnel",
		"--metrics",
		fmt.Sprintf("localhost:%d", metricsPort),
		"--protocol",
		string(s.protocol),
		"run",
		"--token",
		token,
	)
	fmt.Fprintf(logWriter, "cloudflared tunnel --metrics localhost:%d --protocol %s run --token token\n", metricsPort, s.protocol)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	return cmd
}

// waitForProcessExit waits for the process to exit and returns its exit status.
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

// emit sends an event to the event handler if it is set.
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

// emit sends an event to the event handler if it is set.
func (s *Supervisor) emit(event Event) {
	s.mu.Lock()
	handler := s.eventHandler
	s.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}
