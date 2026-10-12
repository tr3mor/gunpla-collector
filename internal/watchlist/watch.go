package watchlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gunpla-collector/internal/store"
)

const (
	maxWatches     = 200
	maxLabelLength = 120
	// maxPriceCents caps a target at €100,000, which also keeps the cents
	// arithmetic far from overflow.
	maxPriceCents = 10_000_000
	// rearmDropPct is how much cheaper than the last alerted price a listing
	// must get to alert again while still under target. It stops a price
	// that wobbles around one value from alerting every day.
	rearmDropPct = 3
)

// Watch is a saved alert on a kit.
type Watch struct {
	ID    int64
	Label string
	Spec  Spec
	// MaxPriceCents is the EUR price at or under which to alert; 0 = none.
	MaxPriceCents int
	OnRestock     bool
	OnNew         bool
	Enabled       bool
}

// InvalidError reports a watch that can't be saved; callers serving user
// input treat it as a bad request.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, a...)}
}

// ErrNotFound is returned for an unknown watch id.
var ErrNotFound = store.ErrWatchNotFound

func fromStore(sw store.Watch) (Watch, error) {
	w := Watch{ID: sw.ID, Label: sw.Label, MaxPriceCents: sw.MaxPriceCents, OnRestock: sw.OnRestock, OnNew: sw.OnNew, Enabled: sw.Enabled}
	if err := json.Unmarshal([]byte(sw.Spec), &w.Spec); err != nil {
		return Watch{}, fmt.Errorf("decode spec of watch %d: %w", sw.ID, err)
	}
	w.Spec = w.Spec.normalized()
	return w, nil
}

func (w Watch) toStore() (store.Watch, error) {
	spec, err := json.Marshal(w.Spec.normalized())
	if err != nil {
		return store.Watch{}, err
	}
	return store.Watch{ID: w.ID, Label: w.Label, Spec: string(spec), MaxPriceCents: w.MaxPriceCents,
		OnRestock: w.OnRestock, OnNew: w.OnNew, Enabled: w.Enabled}, nil
}

func (w Watch) validate() error {
	w.Label = strings.TrimSpace(w.Label)
	switch {
	case w.Label == "":
		return invalid("a watch needs a name")
	case utf8.RuneCountInString(w.Label) > maxLabelLength:
		return invalid("name is longer than %d characters", maxLabelLength)
	case w.MaxPriceCents < 0 || w.MaxPriceCents > maxPriceCents:
		return invalid("target price is out of range")
	case w.MaxPriceCents == 0 && !w.OnRestock && !w.OnNew:
		return invalid("turn on at least one alert: a target price, restock or new listings")
	}
	if err := w.Spec.normalized().Validate(); err != nil {
		return invalid("%s", err.Error())
	}
	return nil
}

// Watches lists the saved watches, oldest first.
func (c *Watchlist) List(ctx context.Context) ([]Watch, error) {
	rows, err := c.store.Watches(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Watch, 0, len(rows))
	for _, r := range rows {
		w, err := fromStore(r)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// Save creates the watch (ID 0) or updates it. An update that changes
// what the watch follows or its price target restarts its alert state, so
// the new settings get a fresh baseline.
func (c *Watchlist) Save(ctx context.Context, w Watch) (Watch, error) {
	w.Label = strings.TrimSpace(w.Label)
	w.Spec = w.Spec.normalized()
	if err := w.validate(); err != nil {
		return Watch{}, err
	}
	sw, err := w.toStore()
	if err != nil {
		return Watch{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if w.ID == 0 {
		existing, err := c.store.Watches(ctx)
		if err != nil {
			return Watch{}, err
		}
		if len(existing) >= maxWatches {
			return Watch{}, invalid("too many watches (limit %d)", maxWatches)
		}
		if w.ID, err = c.store.CreateWatch(ctx, sw, now); err != nil {
			return Watch{}, err
		}
		return w, nil
	}
	old, err := c.store.WatchByID(ctx, w.ID)
	if err != nil {
		return Watch{}, err
	}
	reset := old.Spec != sw.Spec || old.MaxPriceCents != sw.MaxPriceCents || old.Enabled != sw.Enabled
	if err := c.store.UpdateWatch(ctx, sw, reset, now); err != nil {
		return Watch{}, err
	}
	return w, nil
}

// Delete removes a watch.
func (c *Watchlist) Delete(ctx context.Context, id int64) error {
	return c.store.DeleteWatch(ctx, id)
}

// Status is a watch with what it currently resolves to.
type Status struct {
	Watch   Watch
	Preview Preview
	// Hit is true when a buyable listing is at or under the price target.
	Hit bool
}

// Statuses evaluates every watch against one read of the catalog.
func (c *Watchlist) Statuses(ctx context.Context) ([]Status, error) {
	watches, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(watches))
	for _, w := range watches {
		p := snap.preview(w.Spec)
		st := Status{Watch: w, Preview: p}
		if best, ok := p.Best(); ok && w.MaxPriceCents > 0 && best.EURCents <= w.MaxPriceCents {
			st.Hit = true
		}
		out = append(out, st)
	}
	return out, nil
}

// Alert reasons.
const (
	ReasonTarget  = "target"  // a buyable listing is at or under the price target
	ReasonRestock = "restock" // something that was not buyable is again
	ReasonNew     = "new"     // a listing the watch hadn't seen appeared
)

// Alert is one watch with something to tell the user.
type Alert struct {
	Watch   Watch
	Reasons []string
	// Best is the cheapest buyable listing; ok only when one exists.
	Best    Match
	HasBest bool
	// NewListings are the listings that appeared since the last pass (only
	// for ReasonNew).
	NewListings []Match
}

// Pass is the result of evaluating all watches: the alerts to send and
// the state to remember once they were sent.
type Pass struct {
	Alerts  []Alert
	c       *Watchlist
	pending []store.WatchState
}

// Commit stores the evaluated state. Call it after the alerts were
// delivered: if the send fails, the next pass raises the same alerts
// again. It is also needed when there are no alerts (baselines, re-arming).
func (p *Pass) Commit(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var errs []error
	for _, st := range p.pending {
		if err := p.c.store.SaveWatchState(ctx, st, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Evaluate checks every enabled watch against the current catalog.
//
// A watch's first pass only records a baseline, except that a price already
// under target is reported at once (it is actionable). After that it alerts
// on changes: the price reaching the target (again only after leaving it,
// or when it drops a further rearmDropPct), a listing that was not buyable
// being buyable again, and listings not seen before.
func (c *Watchlist) Evaluate(ctx context.Context) (*Pass, error) {
	watches, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	pass := &Pass{c: c}
	if len(watches) == 0 {
		return pass, nil
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range watches {
		if !w.Enabled {
			continue
		}
		prev, seen, err := c.store.WatchState(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		p := snap.preview(w.Spec)
		live := p.Live()
		best, hasBest := p.Best()

		// KnownSets only grows, so a listing that vanishes for one scrape
		// and comes back is not announced as new again.
		next := store.WatchState{WatchID: w.ID, WasBuyable: hasBest, LastAlertCents: prev.LastAlertCents, KnownSets: slices.Clone(prev.KnownSets)}
		for _, m := range live {
			if !slices.Contains(next.KnownSets, m.SetID) {
				next.KnownSets = append(next.KnownSets, m.SetID)
			}
		}
		alert := Alert{Watch: w, Best: best, HasBest: hasBest}

		if hasBest && w.MaxPriceCents > 0 && best.EURCents <= w.MaxPriceCents {
			switch {
			case prev.LastAlertCents == 0 || best.EURCents*100 <= prev.LastAlertCents*(100-rearmDropPct):
				alert.Reasons = append(alert.Reasons, ReasonTarget)
				next.LastAlertCents = best.EURCents
			case best.EURCents > prev.LastAlertCents:
				next.LastAlertCents = best.EURCents // follow a rise, so a later drop alerts
			}
		} else {
			next.LastAlertCents = 0 // above target or nothing to buy: armed again
		}
		// A restock that also lands under the target is one event: the
		// target alert already says the kit is buyable.
		if seen && w.OnRestock && hasBest && !prev.WasBuyable && len(alert.Reasons) == 0 {
			alert.Reasons = append(alert.Reasons, ReasonRestock)
		}
		if seen && w.OnNew {
			for _, m := range live {
				if !slices.Contains(prev.KnownSets, m.SetID) {
					alert.NewListings = append(alert.NewListings, m)
				}
			}
			if len(alert.NewListings) > 0 {
				alert.Reasons = append(alert.Reasons, ReasonNew)
			}
		}
		if len(alert.Reasons) > 0 {
			pass.Alerts = append(pass.Alerts, alert)
		}
		pass.pending = append(pass.pending, next)
	}
	return pass, nil
}
