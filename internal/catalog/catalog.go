// Package catalog owns products: the grouping of one kit's listings across
// shops. It regroups listings after a collect (and on demand), applies
// hand-made links, and serves the read side the search UI needs: filtered
// listings and products with the cheapest one flagged.
package catalog

import (
	"context"
	"sort"
	"time"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/store"
)

// Store is the persistence the catalog needs; *store.Store satisfies it.
type Store interface {
	Listings(ctx context.Context) ([]store.ListingRow, error)
	ApplyProducts(ctx context.Context, plans []store.ProductPlan, now string) (created, removed int, err error)
	LinkSets(ctx context.Context, a, b int64) error
	UnlinkSet(ctx context.Context, id int64, now string) error
	SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error)
}

// Catalog groups listings into products and answers queries over them.
type Catalog struct {
	store Store
	// rates converts listing prices to EUR so listings priced in different
	// currencies can be compared.
	rates fx.Rates
}

// Option configures a Catalog.
type Option func(*Catalog)

// WithEURRates sets the rates used to compare prices across currencies.
func WithEURRates(r fx.Rates) Option { return func(c *Catalog) { c.rates = r } }

func New(s Store, opts ...Option) *Catalog {
	c := &Catalog{store: s}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Suggestion is a near-miss pair Regroup declined to link; confirm one
// with Link.
type Suggestion = matcher.Suggestion

// Report summarises one Regroup.
type Report struct {
	Listings   int
	Products   int // products after the run
	MultiShop  int // products listed by two or more shops
	Created    int // new product rows
	Removed    int // product rows left with no listings
	Reassigned int // listings whose product changed
	ByMethod   map[string]int
	// Suggestions are the near-miss pairs, in the matcher's order (best
	// score first is up to the caller).
	Suggestions []Suggestion
}

// Regroup matches every listing against every other and stores the result.
// A group keeps the product id its members already had (lowest wins; a
// split gets fresh ids), so ids stay stable between runs, and hand-made
// links are never undone.
func (c *Catalog) Regroup(ctx context.Context) (Report, error) {
	rows, err := c.store.Listings(ctx)
	if err != nil {
		return Report{}, err
	}
	listings := make([]matcher.Listing, len(rows))
	for i, r := range rows {
		listings[i] = matcher.Listing{ID: r.ID, Shop: r.Shop, Name: r.Name, Grade: r.Grade, EAN: r.EAN, ProductID: r.ProductID, Manual: r.Manual}
	}
	groups, suggestions := matcher.Match(listings)

	plans, rep := plan(listings, groups)
	rep.Suggestions = suggestions
	created, removed, err := c.store.ApplyProducts(ctx, plans, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return Report{}, err
	}
	rep.Created, rep.Removed = created, removed
	return rep, nil
}

// plan turns matcher groups into the products to store, and fills in the
// parts of the report that come from the groups themselves.
func plan(listings []matcher.Listing, groups []matcher.Group) ([]store.ProductPlan, Report) {
	byID := make(map[int64]matcher.Listing, len(listings))
	for _, l := range listings {
		byID[l.ID] = l
	}
	rep := Report{ByMethod: map[string]int{}, Listings: len(listings)}

	plans := make([]store.ProductPlan, 0, len(groups))
	used := map[int64]bool{}
	for _, g := range groups {
		// Keep the lowest product id a member already has that no earlier
		// group claimed.
		var pid int64
		for _, id := range g.Members {
			if p := byID[id].ProductID; p != 0 && !used[p] && (pid == 0 || p < pid) {
				pid = p
			}
		}
		if pid != 0 {
			used[pid] = true
		}
		name, grade := canonical(g, byID)
		p := store.ProductPlan{ProductID: pid, Name: name, Grade: grade}

		shops := map[string]bool{}
		for _, id := range g.Members {
			l := byID[id]
			shops[l.Shop] = true
			if pid == 0 || l.ProductID != pid {
				rep.Reassigned++
			}
			rep.ByMethod[methodLabel(g.Method[id])]++
			p.Members = append(p.Members, store.ProductMember{SetID: id, Method: g.Method[id]})
		}
		rep.Products++
		if len(shops) > 1 {
			rep.MultiShop++
		}
		plans = append(plans, p)
	}
	return plans, rep
}

func methodLabel(m string) string {
	if m == "" {
		return "single"
	}
	return m
}

// canonical picks a group's display name (the shortest listing name — shops
// pad names with "Bandai Model Kit" style noise) and grade (first non-empty).
func canonical(g matcher.Group, byID map[int64]matcher.Listing) (name, grade string) {
	ids := append([]int64(nil), g.Members...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		l := byID[id]
		if name == "" || len(l.Name) < len(name) {
			name = l.Name
		}
		if grade == "" {
			grade = l.Grade
		}
	}
	return name, grade
}

// Link puts listing b into listing a's product by hand. Both become
// manual, so Regroup leaves them alone from then on.
func (c *Catalog) Link(ctx context.Context, a, b int64) error {
	return c.store.LinkSets(ctx, a, b)
}

// Unlink moves a listing into a product of its own and pins it there.
func (c *Catalog) Unlink(ctx context.Context, id int64) error {
	return c.store.UnlinkSet(ctx, id, time.Now().UTC().Format(time.RFC3339))
}
