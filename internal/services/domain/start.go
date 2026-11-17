package domainservice

import (
	"context"
	"fmt"

	"tunnelmanager/internal/pkg/crypto"
)

// StartDomain launches the cloudflared process for a domain that is not
// currently running (for example after StopDomain or after the supervisor gave
// up retrying). It returns ErrAlreadyRunning while a process is still alive,
// including the short window where a stop has been requested but the process
// has not exited yet.
func (s *domainService) StartDomain(ctx context.Context, id string) error {
	unlock := s.lockDomain(id)
	defer unlock()

	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.sup.IsRunning(id) {
		return ErrAlreadyRunning
	}
	plaintext, err := crypto.Decrypt(s.encKey, domain.EncryptedTunnelToken)
	if err != nil {
		return fmt.Errorf("service: decrypt token: %w", err)
	}
	domain.RestartCount = 0
	domain.LastError = ""
	if err := s.update(ctx, domain); err != nil {
		return err
	}
	return s.spawn(domain, plaintext)
}
