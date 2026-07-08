package config

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
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
	AdminUsername         string
	AdminPassword         string
	JWTSecret             []byte
	JWTTTL                time.Duration
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
		AdminUsername:         v.GetString("ADMIN_USERNAME"),
		AdminPassword:         v.GetString("ADMIN_PASSWORD"),
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
	if cfg.AdminUsername == "" {
		return Config{}, fmt.Errorf("ADMIN_USERNAME is required")
	}
	if cfg.AdminPassword == "" {
		return Config{}, fmt.Errorf("ADMIN_PASSWORD is required")
	}

	key, err := hex.DecodeString(v.GetString("ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, fmt.Errorf("ENCRYPTION_KEY must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes (got %d)", len(key))
	}
	cfg.EncryptionKey = key

	jwtSecret, err := hex.DecodeString(v.GetString("JWT_SECRET"))
	if err != nil {
		return Config{}, fmt.Errorf("JWT_SECRET must be hex-encoded: %w", err)
	}
	if len(jwtSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must decode to at least 32 bytes (got %d)", len(jwtSecret))
	}
	cfg.JWTSecret = jwtSecret

	jwtTTL := v.GetString("JWT_TTL")
	if jwtTTL == "" {
		cfg.JWTTTL = 7 * 24 * time.Hour
	} else {
		ttl, err := time.ParseDuration(jwtTTL)
		if err != nil {
			return Config{}, fmt.Errorf("JWT_TTL must be a valid duration: %w", err)
		}
		cfg.JWTTTL = ttl
	}

	if cfg.MetricsPortRangeEnd <= cfg.MetricsPortRangeStart {
		return Config{}, fmt.Errorf("METRICS_PORT_RANGE_END (%d) must be greater than METRICS_PORT_RANGE_START (%d)", cfg.MetricsPortRangeEnd, cfg.MetricsPortRangeStart)
	}
	if cfg.CloudflaredProtocol != constant.CP_HTTP2 && cfg.CloudflaredProtocol != constant.CP_QUIC {
		return Config{}, fmt.Errorf("CLOUDFLARED_PROTOCOL must be one of http2 or quic (got %q)", cfg.CloudflaredProtocol)
	}

	return cfg, nil
}
