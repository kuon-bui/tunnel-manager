package constant

type DomainStatus string

const (
	StatusPending DomainStatus = "pending"
	StatusActive  DomainStatus = "active"
	StatusError   DomainStatus = "error"
	StatusStopped DomainStatus = "stopped"
)
