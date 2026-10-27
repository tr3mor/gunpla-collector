// Package fx owns currency conversion to EUR. It is used once, when a
// collect run stores prices; everything downstream reads the stored EUR
// value and never converts.
package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

// latestDKKToEURURL serves the ECB's daily reference rate; free, no key.
const latestDKKToEURURL = "https://api.frankfurter.dev/v1/latest?base=DKK&symbols=EUR"

// fetchTimeout bounds the lookup so an unreachable API can't stall `collect`.
const fetchTimeout = 5 * time.Second

// Rates maps a currency code to its value in EUR. EUR itself needs no entry.
type Rates map[string]float64

// ToEUR converts cents in currency to EUR cents, also returning the rate
// used (1 for EUR). ok is false for a currency with no rate: such a price
// has no EUR value and isn't comparable with the others.
func (r Rates) ToEUR(cents int, currency string) (eurCents int, rate float64, ok bool) {
	if currency == "EUR" {
		return cents, 1, true
	}
	rate, found := r[currency]
	if !found {
		return 0, 0, false
	}
	return int(math.Round(float64(cents) * rate)), rate, true
}

// Live returns today's DKK->EUR rate from the ECB reference rates. An
// error means the caller should fall back to a rate it already knows.
func Live(ctx context.Context) (Rates, error) {
	rate, err := fetchDKKToEUR(ctx, http.DefaultClient, latestDKKToEURURL)
	if err != nil {
		return nil, err
	}
	return Rates{"DKK": rate}, nil
}

func fetchDKKToEUR(ctx context.Context, client *http.Client, url string) (float64, error) {
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
	// Sanity bounds: catches a missing field (0) or a garbled response. The
	// krone is pegged to the euro (about 0.134), so a narrow range is safe.
	if rate < 0.05 || rate > 0.5 {
		return 0, fmt.Errorf("implausible DKK/EUR rate %v", rate)
	}
	return rate, nil
}
