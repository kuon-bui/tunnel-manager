package config

import (
	"encoding/hex"
	"fmt"
	"strings"
	"tunnelmanager/internal/pkg/constant"

	"github.com/spf13/viper"
)

type Config struct {
	CloudflareAPIToken    string
	CloudflareAccountID   string
	CloudflareZoneID      string
	EncryptionKey         []byte
	DBPath                string
	LogDir                string
	HTTPAddr              string
	MetricsPortRangeStart int
	MetricsPortRangeEnd   int
	CloudflaredBinary     string
	CloudflaredProtocol   constant.CloudflaredProtocol
	CORSAllowedOrigin     string
}

func Load() (Config, error) {
	v := viper.New()
	v.AutomaticEnv()

	cfg := Config{
		CloudflareAPIToken:    v.GetString("CLOUDFLARE_API_TOKEN"),
		CloudflareAccountID:   v.GetString("CLOUDFLARE_ACCOUNT_ID"),
		CloudflareZoneID:      v.GetString("CLOUDFLARE_ZONE_ID"),
		DBPath:                v.GetString("DB_PATH"),
		LogDir:                v.GetString("LOG_DIR"),
		HTTPAddr:              v.GetString("HTTP_ADDR"),
		MetricsPortRangeStart: v.GetInt("METRICS_PORT_RANGE_START"),
		MetricsPortRangeEnd:   v.GetInt("METRICS_PORT_RANGE_END"),
		CloudflaredBinary:     v.GetString("CLOUDFLARED_BINARY"),
		CloudflaredProtocol:   constant.CloudflaredProtocol(strings.ToLower(v.GetString("CLOUDFLARED_PROTOCOL"))),
		CORSAllowedOrigin:     v.GetString("CORS_ALLOWED_ORIGIN"),
	}

	if cfg.CloudflareAPIToken == "" {
		return Config{}, fmt.Errorf("CLOUDFLARE_API_TOKEN is required")
	}
	if cfg.CloudflareAccountID == "" {
		return Config{}, fmt.Errorf("CLOUDFLARE_ACCOUNT_ID is required")
	}
	if cfg.CloudflareZoneID == "" {
		return Config{}, fmt.Errorf("CLOUDFLARE_ZONE_ID is required")
	}

	key, err := hex.DecodeString(v.GetString("ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, fmt.Errorf("ENCRYPTION_KEY must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes (got %d)", len(key))
	}
	cfg.EncryptionKey = key

	if cfg.MetricsPortRangeEnd <= cfg.MetricsPortRangeStart {
		return Config{}, fmt.Errorf("METRICS_PORT_RANGE_END (%d) must be greater than METRICS_PORT_RANGE_START (%d)", cfg.MetricsPortRangeEnd, cfg.MetricsPortRangeStart)
	}
	if cfg.CloudflaredProtocol != constant.CP_HTTP2 && cfg.CloudflaredProtocol != constant.CP_QUIC {
		return Config{}, fmt.Errorf("CLOUDFLARED_PROTOCOL must be one of http2 or quic (got %q)", cfg.CloudflaredProtocol)
	}

	return cfg, nil
}
