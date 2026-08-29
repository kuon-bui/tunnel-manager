package domainservice

import (
	"context"
	"fmt"
	"time"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/crypto"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/google/uuid"
)

func (s *domainService) CreateDomain(ctx context.Context, hostname, zoneID string, inputs []domainrequest.RouteInput) (domain *model.Domain, err error) {
	revertFuncs := []func(){}
	defer func() {
		if err != nil {
			for i := len(revertFuncs) - 1; i >= 0; i-- {
				revertFuncs[i]()
			}
		}
	}()

	zones, err := s.ListCloudflareZones(ctx)
	if err != nil {
		return nil, err
	}
	hostname, err = validateZoneHostname(zones, zoneID, hostname)
	if err != nil {
		return nil, err
	}

	if existing, _ := s.repo.GetByHostname(ctx, hostname); existing != nil {
		return nil, fmt.Errorf("service: hostname %q already registered", hostname)
	}

	now := time.Now().UTC()
	domainID := uuid.NewString()
	routes, defaultOrigin, err := normalizeRoutes(domainID, inputs, now)
	if err != nil {
		return nil, err
	}

	tunnel, err := s.cf.CreateTunnel(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("service: create tunnel: %w", err)
	}

	revertFuncs = append(revertFuncs, func() {
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
	})

	if err := s.proxy.CommitDomain(domainID, hostname, routes); err != nil {
		return nil, fmt.Errorf("service: prepare ingress proxy: %w", err)
	}
	revertFuncs = append(revertFuncs, func() { s.proxy.RemoveDomain(domainID) })

	if err := s.cf.PutIngressConfig(ctx, tunnel.TunnelID, hostname, s.cloudflareRules(routes)); err != nil {
		return nil, fmt.Errorf("%w: put ingress config", ErrCloudflareUnavailable)
	}

	dnsRecordID, err := s.cf.CreateDNSRecord(ctx, zoneID, hostname, tunnel.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("service: create dns record: %w", err)
	}
	revertFuncs = append(revertFuncs, func() {
		_ = s.cf.DeleteDNSRecord(ctx, zoneID, dnsRecordID)
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

	domain = &model.Domain{
		ID:                   domainID,
		Hostname:             hostname,
		OriginURL:            defaultOrigin,
		CloudflareZoneID:     zoneID,
		CloudflareTunnelID:   tunnel.TunnelID,
		DNSRecordID:          dnsRecordID,
		EncryptedTunnelToken: encToken,
		Status:               constant.StatusPending,
		MetricsPort:          port,
		CreatedAt:            now,
		UpdatedAt:            now,
		Routes:               routes,
	}
	if err := s.create(ctx, domain); err != nil {
		return nil, fmt.Errorf("service: persist domain: %w", err)
	}

	if err := s.spawn(domain, tunnel.Token); err != nil {
		domain.Status = constant.StatusError
		domain.LastError = err.Error()
		_ = s.update(ctx, domain)
		return domain, nil
	}

	return domain, nil
}

func (s *domainService) spawn(domain *model.Domain, plaintextToken string) error {
	logWriter, err := s.logBuffer(domain.ID)
	if err != nil {
		return fmt.Errorf("open log buffer: %w", err)
	}
	if err := s.sup.Start(domain.ID, plaintextToken, domain.MetricsPort, logWriter); err != nil {
		return err
	}
	return nil
}
