package domain

import (
	"context"
	"fmt"
	"tunnelmanager/internal/crypto"
)

func (s *domainService) RestartDomain(ctx context.Context, id string) error {
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
