package domain

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
	"tunnelmanager/internal/crypto"
	"tunnelmanager/internal/logbuf"
	"tunnelmanager/internal/model"

	"github.com/google/uuid"
)

func (s *domainService) takenPorts(ctx context.Context) (map[int]bool, error) {
	domains, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	taken := make(map[int]bool, len(domains))
	for _, d := range domains {
		taken[d.MetricsPort] = true
	}
	return taken, nil
}

func (s *domainService) CreateDomain(ctx context.Context, hostname, originURL string) (domain *model.Domain, err error) {

	if existing, _ := s.repo.GetByHostname(ctx, hostname); existing != nil {
		return nil, fmt.Errorf("service: hostname %q already registered", hostname)
	}

	tunnel, err := s.cf.CreateTunnel(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("service: create tunnel: %w", err)
	}

	defer func() {
		if err != nil {
			_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		}
	}()

	if err := s.cf.PutIngressConfig(ctx, tunnel.TunnelID, hostname, originURL); err != nil {
		return nil, fmt.Errorf("service: put ingress config: %w", err)
	}

	dnsRecordID, err := s.cf.CreateDNSRecord(ctx, hostname, tunnel.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("service: create dns record: %w", err)
	}
	defer func() {
		if err != nil {
			_ = s.cf.DeleteDNSRecord(ctx, dnsRecordID)
		}
	}()
	encToken, err := crypto.Encrypt(s.encKey, tunnel.Token)
	if err != nil {
		return nil, fmt.Errorf("service: encrypt token: %w", err)
	}

	taken, err := s.takenPorts(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: list taken ports: %w", err)
	}
	port, err := s.ports.Allocate(taken)
	if err != nil {
		return nil, fmt.Errorf("service: allocate metrics port: %w", err)
	}

	now := time.Now().UTC()
	domain = &model.Domain{
		ID:                   uuid.NewString(),
		Hostname:             hostname,
		OriginURL:            originURL,
		CloudflareTunnelID:   tunnel.TunnelID,
		DNSRecordID:          dnsRecordID,
		EncryptedTunnelToken: encToken,
		Status:               model.StatusPending,
		MetricsPort:          port,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := s.repo.Create(ctx, domain); err != nil {
		return nil, fmt.Errorf("service: persist domain: %w", err)
	}

	if err := s.spawn(domain, tunnel.Token); err != nil {
		domain.Status = model.StatusError
		domain.LastError = err.Error()
		_ = s.repo.Update(ctx, domain)
		return domain, nil
	}

	return domain, nil
}

func (s *domainService) spawn(domain *model.Domain, plaintextToken string) error {
	logPath := filepath.Join(s.logDir, domain.ID+".log")
	logWriter, err := logbuf.NewBuffer(logPath, 500)
	if err != nil {
		return fmt.Errorf("open log buffer: %w", err)
	}
	if err := s.sup.Start(domain.ID, plaintextToken, domain.MetricsPort, logWriter); err != nil {
		_ = logWriter.Close()
		return err
	}
	s.mu.Lock()
	if old, ok := s.logs[domain.ID]; ok {
		_ = old.Close()
	}
	s.logs[domain.ID] = logWriter
	s.mu.Unlock()
	return nil
}
