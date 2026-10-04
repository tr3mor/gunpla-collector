// Package fx converts shop prices to EUR. Stored prices stay in the shop's
// own currency; callers convert at presentation time (reports, search UI).
package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"
)

// latestUSDToEURURL serves the ECB's daily reference rate; free, no key.
const latestUSDToEURURL = "https://api.frankfurter.dev/v1/latest?base=USD&symbols=EUR"

// fetchTimeout bounds the startup lookup so an unreachable API can't stall
// `serve` or `report`.
const fetchTimeout = 5 * time.Second

// Rates maps a currency code to its value in EUR. EUR itself and
// currencies with no entry pass through unconverted.
type Rates map[string]float64

// ToEUR converts cents in currency to EUR cents. ok is false (and the
// input returned unchanged) when the currency is already EUR or unknown.
func (r Rates) ToEUR(cents int, currency string) (eurCents int, ok bool) {
	rate, found := r[currency]
	if !found || currency == "EUR" {
		return cents, false
	}
	return int(math.Round(float64(cents) * rate)), true
}

// Load returns the rates to use for this process: today's live USD->EUR
// rate, or fallbackUSD if the lookup fails (logged, never fatal).
func Load(ctx context.Context, fallbackUSD float64, logger *slog.Logger) Rates {
	rate, err := fetchUSDToEUR(ctx, http.DefaultClient, latestUSDToEURURL)
	if err != nil {
		logger.Warn("could not fetch live USD/EUR rate, using fallback", "rate", fallbackUSD, "error", err)
		return Rates{"USD": fallbackUSD}
	}
	logger.Info("using live USD/EUR rate", "rate", rate)
	return Rates{"USD": rate}
}

func fetchUSDToEUR(ctx context.Context, client *http.Client, url string) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %s", resp.Status)
	}
	var body struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<16)).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	rate := body.Rates["EUR"]
	// Sanity bounds: catches a missing field (0) or a garbled response
	// without hard-coding where the dollar "should" be.
	if rate < 0.3 || rate > 3 {
		return 0, fmt.Errorf("implausible USD/EUR rate %v", rate)
	}
	return rate, nil
}
