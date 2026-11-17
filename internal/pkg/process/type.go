package process

import (
	"io"
	"os/exec"
	"sync"
	"time"
	"tunnelmanager/internal/pkg/constant"
)

const maxRestartAttempts = 5

type Event = ProcessEvent

type managedProcess struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	stopRequested bool
	restartCount  int
	stableTimer   *time.Timer

	// stopCh is closed when a stop is requested so a pending restart backoff
	// is interrupted instead of spawning a new process.
	stopCh   chan struct{}
	stopOnce sync.Once
	// done is closed once supervision has ended and the final event has been
	// emitted.
	done chan struct{}
}

func newManagedProcess() *managedProcess {
	return &managedProcess{stopCh: make(chan struct{}), done: make(chan struct{})}
}

type Supervisor struct {
	binary       string
	protocol     constant.CloudflaredProtocol
	eventHandler func(Event)

	StableAfter time.Duration

	mu    sync.Mutex
	procs map[string]*managedProcess
}

type ProcessEvent struct {
	DomainID     string
	PID          int
	Status       constant.DomainStatus
	RestartCount int
	Err          error
}

type ProcessSupervisor interface {
	Start(domainID, token string, metricsPort int, logWriter io.Writer) error
	Stop(domainID string) error
	// StopAndWait stops the process and blocks until it has exited and its
	// final stopped event has been emitted. If the process has not exited
	// after grace, it is killed. It is a no-op when nothing is running.
	StopAndWait(domainID string, grace time.Duration) error
	IsRunning(domainID string) bool
	SetEventHandler(handler func(ProcessEvent))
}
