package ports

import (
	"io"

	"tunnelmanager/internal/model"
)

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
