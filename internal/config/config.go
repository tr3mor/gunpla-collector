// Package config loads 12-factor style configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBPath         string
	TelegramToken  string
	TelegramChatID string
	Shops          []string // empty = all active shops
	// RunTimeout bounds a whole collect/report invocation. Zero means "use
	// the command's own default" (see main.go).
	RunTimeout time.Duration
	// Force is the env-var equivalent of collect's -force flag, for
	// overriding the sanity guard from `docker compose exec`.
	Force bool
}

func Load() Config {
	return Config{
		DBPath:         getEnv("GUNPLA_DB_PATH", "/data/gunpla.db"),
		TelegramToken:  os.Getenv("GUNPLA_TELEGRAM_BOT_TOKEN"),
		TelegramChatID: os.Getenv("GUNPLA_TELEGRAM_CHAT_ID"),
		Shops:          parseShops(os.Getenv("GUNPLA_SHOPS")),
		RunTimeout:     parseRunTimeout(os.Getenv("GUNPLA_RUN_TIMEOUT")),
		Force:          parseBool(os.Getenv("GUNPLA_FORCE")),
	}
}

// parseBool treats unset or unparsable as false — this package has no
// logger to report a bad value through.
func parseBool(raw string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && b
}

// parseRunTimeout parses a Go duration string (e.g. "45m", "1h"). Empty or
// unparsable falls back to 0 ("use the command's default") rather than
// failing the run over a typo.
func parseRunTimeout(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

func parseShops(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	shops := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			shops = append(shops, p)
		}
	}
	return shops
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ValidateForReport fails fast if Telegram credentials are missing, since
// `report` cannot do anything useful without them.
func (c Config) ValidateForReport() error {
	if c.TelegramToken == "" {
		return fmt.Errorf("GUNPLA_TELEGRAM_BOT_TOKEN is required for report")
	}
	if c.TelegramChatID == "" {
		return fmt.Errorf("GUNPLA_TELEGRAM_CHAT_ID is required for report")
	}
	return nil
}
