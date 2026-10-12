package main

import (
	"context"
	"log/slog"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/store"
)

// loadRates picks the rates a collect run converts prices with, and fills in
// EUR values for rows stored without one. Prices keep the rate they were
// stored with, so a bad rate would stay in history; hence the order: today's
// live ECB rate, else the rate the last run used, else the configured
// fallback (only ever reached on the very first run).
func loadRates(ctx context.Context, db *store.Store, live func(context.Context) (fx.Rates, error), fallbackUSD float64, logger *slog.Logger) fx.Rates {
	rates, err := live(ctx)
	if err == nil {
		logger.Info("using live USD/EUR rate", "rate", rates["USD"])
	} else {
		last, ok, lerr := db.LastRate(ctx, "USD")
		if lerr != nil {
			logger.Warn("could not read the last stored USD/EUR rate", "error", lerr)
		}
		if ok {
			rates = fx.Rates{"USD": last}
			logger.Warn("could not fetch live USD/EUR rate, reusing the last one stored", "rate", last, "error", err)
		} else {
			rates = fx.Rates{"USD": fallbackUSD}
			logger.Warn("could not fetch live USD/EUR rate, using the configured fallback", "rate", fallbackUSD, "error", err)
		}
	}
	if n, err := db.BackfillEUR(ctx, rates); err != nil {
		logger.Warn("backfill of EUR prices failed", "error", err)
	} else if n > 0 {
		logger.Info("backfilled EUR prices on older rows", "rows", n)
	}
	return rates
}
