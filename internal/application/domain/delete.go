package domain

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
)

func (s *domainService) DeleteDomain(ctx context.Context, id string) error {
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.sup.IsRunning(id) {
		if err := s.sup.Stop(id); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("service: stop process: %w", err)
		}
	}
	if err := s.cf.DeleteDNSRecord(ctx, domain.DNSRecordID); err != nil {
		log.Printf("service: delete domain %s: delete dns record %s failed, continuing with best-effort cleanup: %v", id, domain.DNSRecordID, err)
	}
	if err := s.cf.DeleteTunnel(ctx, domain.CloudflareTunnelID); err != nil {
		log.Printf("service: delete domain %s: delete tunnel %s failed, continuing with best-effort cleanup: %v", id, domain.CloudflareTunnelID, err)
	}
	return s.repo.Delete(ctx, id)
}
