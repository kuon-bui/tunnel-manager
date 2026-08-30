package domainservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
)

func (s *domainService) DeleteDomain(ctx context.Context, id string) error {
	unlock := s.lockDomain(id)
	defer unlock()

	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	routes, err := s.repo.ListRoutes(ctx, id)
	if err != nil {
		return err
	}
	if s.sup.IsRunning(id) {
		if err := s.sup.Stop(id); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("service: stop process: %w", err)
		}
	}
	if err := s.cf.DeleteDNSRecord(ctx, domain.CloudflareZoneID, domain.DNSRecordID); err != nil {
		log.Printf("service: delete domain %s: delete dns record %s failed, continuing with best-effort cleanup: %v", id, domain.DNSRecordID, err)
	}
	if err := s.cf.DeleteTunnel(ctx, domain.CloudflareTunnelID); err != nil {
		log.Printf("service: delete domain %s: delete tunnel %s failed, continuing with best-effort cleanup: %v", id, domain.CloudflareTunnelID, err)
	}
	s.proxy.RemoveDomain(id)
	if err := s.delete(ctx, id); err != nil {
		_ = s.proxy.RollbackDomain(id, domain.Hostname, routes)
		return err
	}
	s.mu.Lock()
	buf := s.logs[id]
	delete(s.logs, id)
	s.mu.Unlock()
	if buf != nil {
		if err := buf.Close(); err != nil {
			log.Printf("service: delete domain %s: close log buffer failed: %v", id, err)
		}
	}
	return nil
}
