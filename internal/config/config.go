// Package config loads 12-factor style configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DBPath         string
	TelegramToken  string
	TelegramChatID string
	Shops          []string // empty = all active shops
	LogLevel       string
}

func Load() Config {
	return Config{
		DBPath:         getEnv("GUNPLA_DB_PATH", "/data/gunpla.db"),
		TelegramToken:  os.Getenv("GUNPLA_TELEGRAM_BOT_TOKEN"),
		TelegramChatID: os.Getenv("GUNPLA_TELEGRAM_CHAT_ID"),
		Shops:          parseShops(os.Getenv("GUNPLA_SHOPS")),
		LogLevel:       getEnv("GUNPLA_LOG_LEVEL", "info"),
	}
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
