// Command gunpla-collector scrapes Gunpla catalog/price data from
// configured shops (`collect`) and sends a Telegram diff report
// (`report`).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gunpla-collector/internal/collector"
	"gunpla-collector/internal/config"
	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/reporter"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
	"gunpla-collector/internal/telegram"
	"gunpla-collector/internal/web"
)

// Default whole-run timeouts when GUNPLA_RUN_TIMEOUT isn't set — a
// backstop against a hung request or stalled Telegram send. collect gets
// more room since it makes many rate-limited requests across shops.
const (
	defaultCollectTimeout = 30 * time.Minute
	defaultReportTimeout  = 5 * time.Minute
	// matchTimeout bounds the product-grouping step run at the end of
	// collect; it takes well under a second for a few thousand listings.
	matchTimeout = 2 * time.Minute
)

const usageText = `usage: gunpla-collector <collect|report|serve|match|link|unlink> [-shop=<slug>] [-force] [-addr=<addr>]

  -shop string
        shop slug to run against (default: all active shops, or $GUNPLA_SHOPS)
  -force
        collect only: skip the sanity guard that refuses a run returning
        less than half the previous run's set count (also settable via
        $GUNPLA_FORCE=1)
  -addr string
        serve only: address to listen on, e.g. ":8080" (default: $GUNPLA_UI_ADDR
        or ":8080")

  match            group listings of the same kit across shops into products
                   (collect already runs this at its end; use it to re-run on
                   its own after link/unlink or a matcher change)
  link <a> <b>     force set <b> into set <a>'s product (set ids as printed by
                   match); automatic matching won't undo it
  unlink <id>      move a set into a product of its own and pin it there`

func init() {
	scraper.Register(scraper.NewGeeksHeaven())
	scraper.Register(scraper.NewGundamStore())
	scraper.Register(scraper.NewPlamoDX())
	scraper.Register(scraper.NewZeonMarket())
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
	switch cmd {
	case "collect", "report", "serve", "match", "link", "unlink":
	default:
		return usageError()
	}

	flags, err := parseFlags(cmd, args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, usageText)
			return nil
		}
		return fmt.Errorf("%w\n\n%s", err, usageText)
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
	case "match":
		return runMatch(ctx, db, os.Stdout)
	case "link", "unlink":
		return runLink(ctx, db, cmd, flags.args)
	}

	if cmd == "serve" {
		return serve(ctx, db, logger, addrFlag(flags.addr, cfg.UIAddr), fx.Load(ctx, cfg.USDToEUR, logger))
	}

	sigCtx := ctx // outlives the run timeout, which a long collect may exhaust
	ctx, cancel := context.WithTimeout(ctx, runTimeout(cmd, cfg.RunTimeout))
	defer cancel()

	switch cmd {
	case "collect":
		shops, err := resolveShops(ctx, db, cfg, flags.shop)
		if err != nil {
			return err
		}
		opts := collector.Options{Force: flags.force || cfg.Force}
		var firstErr error
		for _, shop := range shops {
			s, ok := scraper.Get(shop.Slug)
			if !ok {
				logger.Warn("no scraper registered for shop, skipping", "shop", shop.Slug)
				continue
			}
			logger.Info("starting collect", "shop", shop.Slug)
			if err := collector.Run(ctx, db, shop, s, logger, opts); err != nil {
				logger.Error("collect failed", "shop", shop.Slug, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		// Group the fresh listings into products before `report` runs.
		matchCtx, cancelMatch := context.WithTimeout(sigCtx, matchTimeout)
		defer cancelMatch()
		matchAfterCollect(matchCtx, db, logger)
		return firstErr

	case "report":
		if err := cfg.ValidateForReport(); err != nil {
			return err
		}
		shops, err := resolveShops(ctx, db, cfg, flags.shop)
		if err != nil {
			return err
		}
		tg := telegram.NewClient(cfg.TelegramToken, cfg.TelegramChatID)
		rates := fx.Load(ctx, cfg.USDToEUR, logger)
		var firstErr error
		for _, shop := range shops {
			logger.Info("starting report", "shop", shop.Slug)
			if err := reporter.Run(ctx, db, shop, tg, logger, reporter.WithEURRates(rates)); err != nil {
				logger.Error("report failed", "shop", shop.Slug, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		return firstErr
	}

	panic("unreachable: cmd validated to be collect or report above")
}

// resolveShops picks which shops to run: -shop flag wins, then
// GUNPLA_SHOPS, then every active shop that still has a scraper
// registered in this binary — so retiring a shop from the code stops it
// being collected/reported without a DB change too. Shops named by flag
// or env var get registered in the DB on first use.
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

	// No explicit selection: register every scraper's shop row (sorted by
	// slug for deterministic order), then keep only the active ones we
	// still have a scraper for.
	for _, s := range scraper.All() {
		if _, err := db.GetOrCreateShop(ctx, s.ShopSlug(), s.ShopName(), s.BaseURL()); err != nil {
			return nil, err
		}
	}
	active, err := db.ActiveShops(ctx)
	if err != nil {
		return nil, err
	}
	shops := make([]store.Shop, 0, len(active))
	for _, shop := range active {
		if _, ok := scraper.Get(shop.Slug); ok {
			shops = append(shops, shop)
		}
	}
	return shops, nil
}

// runTimeout picks the whole-run timeout for cmd: the GUNPLA_RUN_TIMEOUT
// override if set, otherwise the command's own default.
func runTimeout(cmd string, override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if cmd == "report" {
		return defaultReportTimeout
	}
	return defaultCollectTimeout
}

func usageError() error {
	return fmt.Errorf("%s", usageText)
}

// parsedFlags holds one invocation's flag values.
type parsedFlags struct {
	shop  string
	force bool
	addr  string
	args  []string // positional arguments after the flags
}

// parseFlags parses everything after the subcommand. An error wrapping
// flag.ErrHelp means -h was given, not a real failure — the caller
// decides how to present that.
//
// Split out from run() so it's testable without touching the DB. Using
// flag.NewFlagSet also fixes the old parser silently ignoring "-shop x"
// (it only understood "-shop=x").
func parseFlags(cmd string, args []string) (parsedFlags, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // run() formats errors/usage itself
	shopFlag := fs.String("shop", "", "shop slug to run against (default: all active shops)")
	forceFlag := fs.Bool("force", false, "collect only: skip the sanity guard")
	addrFlagVal := fs.String("addr", "", "serve only: address to listen on")
	if err := fs.Parse(args); err != nil {
		return parsedFlags{}, err
	}
	return parsedFlags{shop: *shopFlag, force: *forceFlag, addr: *addrFlagVal, args: fs.Args()}, nil
}

// addrFlag picks the address `serve` listens on: the -addr flag if given,
// otherwise the configured default.
func addrFlag(flagVal, cfgDefault string) string {
	if flagVal != "" {
		return flagVal
	}
	return cfgDefault
}

// serve runs the search UI's HTTP server until ctx is cancelled (SIGINT/SIGTERM),
// then shuts it down gracefully. Unlike collect/report, it has no run-timeout —
// it's meant to stay up.
func serve(ctx context.Context, db *store.Store, logger *slog.Logger, addr string, rates fx.Rates) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: web.NewServer(db, logger, web.WithEURRates(rates)).Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("serving UI", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down server: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}
