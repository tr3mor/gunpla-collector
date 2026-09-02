// Command gunpla-collector scrapes Gunpla catalog/price data from
// configured shops (`collect`) and sends a Telegram diff report
// (`report`).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gunpla-collector/internal/collector"
	"gunpla-collector/internal/config"
	"gunpla-collector/internal/reporter"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
	"gunpla-collector/internal/telegram"
)

func init() {
	scraper.Register(scraper.NewGeeksHeaven())
	scraper.Register(scraper.NewGundamStore())
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	cmd := args[0]
	shopFlag := ""
	for i := 1; i < len(args); i++ {
		if v, ok := flagValue(args[i], "--shop"); ok {
			shopFlag = v
		}
	}

	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			logger.Warn("close store", "error", cerr)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "collect":
		shops, err := resolveShops(ctx, db, cfg, shopFlag)
		if err != nil {
			return err
		}
		var firstErr error
		for _, shop := range shops {
			s, ok := scraper.Get(shop.Slug)
			if !ok {
				logger.Warn("no scraper registered for shop, skipping", "shop", shop.Slug)
				continue
			}
			logger.Info("starting collect", "shop", shop.Slug)
			if err := collector.Run(ctx, db, shop, s, logger); err != nil {
				logger.Error("collect failed", "shop", shop.Slug, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		return firstErr

	case "report":
		if err := cfg.ValidateForReport(); err != nil {
			return err
		}
		shops, err := resolveShops(ctx, db, cfg, shopFlag)
		if err != nil {
			return err
		}
		tg := telegram.NewClient(cfg.TelegramToken, cfg.TelegramChatID)
		var firstErr error
		for _, shop := range shops {
			logger.Info("starting report", "shop", shop.Slug)
			if err := reporter.Run(ctx, db, shop, tg, logger); err != nil {
				logger.Error("report failed", "shop", shop.Slug, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		return firstErr

	default:
		return usageError()
	}
}

// resolveShops determines which shops to operate on: --shop flag wins,
// then GUNPLA_SHOPS, then all active shops in the DB. Shops named by flag
// or env var are registered (created) in the DB on first use, using the
// registered scraper's shop metadata.
func resolveShops(ctx context.Context, db *store.Store, cfg config.Config, shopFlag string) ([]store.Shop, error) {
	var slugs []string
	if shopFlag != "" {
		slugs = []string{shopFlag}
	} else if len(cfg.Shops) > 0 {
		slugs = cfg.Shops
	}

	if len(slugs) > 0 {
		shops := make([]store.Shop, 0, len(slugs))
		for _, slug := range slugs {
			s, ok := scraper.Get(slug)
			if !ok {
				return nil, fmt.Errorf("no scraper registered for shop %q", slug)
			}
			shop, err := db.GetOrCreateShop(ctx, slug, s.ShopName(), s.BaseURL())
			if err != nil {
				return nil, err
			}
			shops = append(shops, shop)
		}
		return shops, nil
	}

	// No explicit selection: ensure every registered scraper has a shop
	// row, then run all active shops.
	for _, s := range scraper.All() {
		if _, err := db.GetOrCreateShop(ctx, s.ShopSlug(), s.ShopName(), s.BaseURL()); err != nil {
			return nil, err
		}
	}
	return db.ActiveShops(ctx)
}

func flagValue(arg, name string) (string, bool) {
	prefix := name + "="
	if len(arg) > len(prefix) && arg[:len(prefix)] == prefix {
		return arg[len(prefix):], true
	}
	return "", false
}

func usageError() error {
	return fmt.Errorf("usage: gunpla-collector <collect|report> [--shop=<slug>]")
}
