package domainservice

import (
	"context"
	"fmt"
	"time"

	"tunnelmanager/internal/pkg/crypto"
)

// restartStopGrace is how long a running cloudflared process gets to shut down
// gracefully during a restart before it is killed.
const restartStopGrace = 10 * time.Second

// RestartDomain stops the domain's cloudflared process (if running), waits for
// it to exit and starts a fresh one. A stopped or errored domain is simply
// started.
func (s *domainService) RestartDomain(ctx context.Context, id string) error {
	unlock := s.lockDomain(id)
	defer unlock()

	if _, err := s.repo.Get(ctx, id); err != nil {
		return err
	}
	if s.sup.IsRunning(id) {
		if err := s.sup.StopAndWait(id, restartStopGrace); err != nil {
			return fmt.Errorf("service: stop process: %w", err)
		}
	}
	// Re-read: the stop event has updated status/PID while we waited.
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
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
