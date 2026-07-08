package domainservice

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/crypto"
	"tunnelmanager/internal/pkg/logbuf"

	"github.com/google/uuid"
)

func (s *domainService) CreateDomain(ctx context.Context, hostname, originURL string) (domain *model.Domain, err error) {
	revertFuncs := []func(){}
	defer func() {
		if err != nil {
			for i := len(revertFuncs) - 1; i >= 0; i-- {
				revertFuncs[i]()
			}
		}
	}()

	if existing, _ := s.repo.GetByHostname(ctx, hostname); existing != nil {
		return nil, fmt.Errorf("service: hostname %q already registered", hostname)
	}

	tunnel, err := s.cf.CreateTunnel(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("service: create tunnel: %w", err)
	}

	revertFuncs = append(revertFuncs, func() {
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
	})

	if err := s.cf.PutIngressConfig(ctx, tunnel.TunnelID, hostname, originURL); err != nil {
		return nil, fmt.Errorf("service: put ingress config: %w", err)
	}

	dnsRecordID, err := s.cf.CreateDNSRecord(ctx, hostname, tunnel.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("service: create dns record: %w", err)
	}
	revertFuncs = append(revertFuncs, func() {
		_ = s.cf.DeleteDNSRecord(ctx, dnsRecordID)
	})

	encToken, err := crypto.Encrypt(s.encKey, tunnel.Token)
	if err != nil {
		return nil, fmt.Errorf("service: encrypt token: %w", err)
	}

	taken, err := s.repo.ListTakenPorts(ctx)
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
		Status:               constant.StatusPending,
		MetricsPort:          port,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := s.repo.Create(ctx, domain); err != nil {
		return nil, fmt.Errorf("service: persist domain: %w", err)
	}

	if err := s.spawn(domain, tunnel.Token); err != nil {
		domain.Status = constant.StatusError
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
