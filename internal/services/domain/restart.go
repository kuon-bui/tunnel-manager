package domainservice

import (
	"context"
	"fmt"
	"tunnelmanager/internal/pkg/crypto"
)

func (s *domainService) RestartDomain(ctx context.Context, id string) (err error) {
	defer func() {
		if err != nil {
			fmt.Println("failed to restart domain", "id", id, "error", err)
		}
	}()
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
	if err := s.repo.Update(ctx, domain); err != nil {
		return err
	}
	return s.spawn(domain, plaintext)
}
