package config

import (
	"strings"
	"testing"
)

func TestLoadDoesNotRequireCloudflareZoneID(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account")
	t.Setenv("CLOUDFLARE_ZONE_ID", "")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("01", 32))
	t.Setenv("DB_PATH", "test.db")
	t.Setenv("LOG_DIR", t.TempDir())
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("METRICS_PORT_RANGE_START", "20500")
	t.Setenv("METRICS_PORT_RANGE_END", "20501")
	t.Setenv("CLOUDFLARED_BINARY", "cloudflared")
	t.Setenv("CLOUDFLARED_PROTOCOL", "http2")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")
	t.Setenv("JWT_SECRET", strings.Repeat("02", 32))
	t.Setenv("JWT_TTL", "1h")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IngressProxyAddr != "127.0.0.1:20080" {
		t.Fatalf("IngressProxyAddr = %q", cfg.IngressProxyAddr)
	}
}

func TestLoadRejectsPublicIngressProxyAddress(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("01", 32))
	t.Setenv("DB_PATH", "test.db")
	t.Setenv("LOG_DIR", t.TempDir())
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("METRICS_PORT_RANGE_START", "20500")
	t.Setenv("METRICS_PORT_RANGE_END", "20501")
	t.Setenv("CLOUDFLARED_BINARY", "cloudflared")
	t.Setenv("CLOUDFLARED_PROTOCOL", "http2")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")
	t.Setenv("JWT_SECRET", strings.Repeat("02", 32))
	t.Setenv("INGRESS_PROXY_ADDR", "0.0.0.0:20080")

	if _, err := Load(); err == nil {
		t.Fatal("expected public ingress proxy address to fail")
	}
}
