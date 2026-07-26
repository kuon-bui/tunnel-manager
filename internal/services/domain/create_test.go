package domainservice

import (
	"bytes"
	"context"
	"testing"

	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/logbuf"
	"tunnelmanager/internal/pkg/portalloc"
)

func TestCreateDomainAddsOptionalIngressPath(t *testing.T) {
	repo := &syncTestRepository{}
	cf := &syncTestCloudflare{
		tunnel: cloudflare.TunnelInfo{
			TunnelID: "tunnel-id",
			Token:    "tunnel-token",
		},
	}
	supervisor := &syncTestSupervisor{}
	service := &domainService{
		repo: repo,
		cf:   cf,
		sup:  supervisor,
		ports: portalloc.NewPortAllocator(config.Config{
			MetricsPortRangeStart: 20500,
			MetricsPortRangeEnd:   20501,
		}),
		encKey: bytes.Repeat([]byte{1}, 32),
		logDir: t.TempDir(),
		logs:   make(map[string]*logbuf.Buffer),
	}

	domain, err := service.CreateDomain(
		context.Background(),
		"app.example.com",
		"http://app:8080",
		"/api/.*",
	)
	if err != nil {
		t.Fatalf("CreateDomain() error = %v", err)
	}
	t.Cleanup(func() {
		service.mu.Lock()
		defer service.mu.Unlock()
		for _, buffer := range service.logs {
			_ = buffer.Close()
		}
	})

	if cf.putHostname != "app.example.com" ||
		cf.putOriginURL != "http://app:8080" ||
		cf.putPath != "/api/.*" {
		t.Fatalf(
			"PutIngressConfig() got hostname=%q originURL=%q path=%q",
			cf.putHostname,
			cf.putOriginURL,
			cf.putPath,
		)
	}
	if repo.created == nil {
		t.Fatal("repository did not receive the created domain")
	}
	if domain.Path != "/api/.*" || repo.created.Path != "/api/.*" {
		t.Fatalf("persisted path = %q, response path = %q", repo.created.Path, domain.Path)
	}
}
