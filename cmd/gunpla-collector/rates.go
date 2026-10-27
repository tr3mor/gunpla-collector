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
func loadRates(ctx context.Context, db *store.Store, live func(context.Context) (fx.Rates, error), fallbackDKK float64, logger *slog.Logger) fx.Rates {
	rates, err := live(ctx)
	if err == nil {
		logger.Info("using live DKK/EUR rate", "rate", rates["DKK"])
	} else {
		last, ok, lerr := db.LastRate(ctx, "DKK")
		if lerr != nil {
			logger.Warn("could not read the last stored DKK/EUR rate", "error", lerr)
		}
		if ok {
			rates = fx.Rates{"DKK": last}
			logger.Warn("could not fetch live DKK/EUR rate, reusing the last one stored", "rate", last, "error", err)
		} else {
			rates = fx.Rates{"DKK": fallbackDKK}
			logger.Warn("could not fetch live DKK/EUR rate, using the configured fallback", "rate", fallbackDKK, "error", err)
		}
	}
	if n, err := db.BackfillEUR(ctx, rates); err != nil {
		logger.Warn("backfill of EUR prices failed", "error", err)
	} else if n > 0 {
		logger.Info("backfilled EUR prices on older rows", "rows", n)
	}
	return rates
}
