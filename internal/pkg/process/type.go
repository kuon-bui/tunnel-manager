package process

import (
	"io"
	"os/exec"
	"sync"
	"time"
	"tunnelmanager/internal/model"
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
}

type Supervisor struct {
	binary       string
	protocol     constant.CloudflaredProtocol
	eventHandler func(Event)

	SleepFunc   func(time.Duration)
	StableAfter time.Duration

	mu    sync.Mutex
	procs map[string]*managedProcess
}

type ProcessEvent struct {
	DomainID     string
	PID          int
	Status       model.Status
	RestartCount int
	Err          error
}

type ProcessSupervisor interface {
	Start(domainID, token string, metricsPort int, logWriter io.Writer) error
	Stop(domainID string) error
	IsRunning(domainID string) bool
	SetEventHandler(handler func(ProcessEvent))
}
