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
	// RunTimeout overrides how long a single `collect` or `report`
	// invocation is allowed to run before its context is cancelled. Zero
	// means "use the command's own default" (see main.go) — a whole-run
	// backstop against a hung HTTP request or a stalled Telegram send,
	// distinct from (and larger than) any single request's own timeout.
	RunTimeout time.Duration
	// Force is the env-var equivalent of collect's -force flag, so the
	// sanity guard can be overridden from `docker compose exec` without
	// editing the container's command.
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

// parseBool is lenient the same way parseRunTimeout is: unset or
// unparsable is just "false", not an error this package has no logger to
// report through.
func parseBool(raw string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && b
}

// parseRunTimeout parses a Go duration string (e.g. "45m", "1h"). An empty
// or unparsable value returns 0, meaning "use the command's default" — this
// package has no logger to report a malformed value through, so a typo
// silently falls back rather than failing the whole run.
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
