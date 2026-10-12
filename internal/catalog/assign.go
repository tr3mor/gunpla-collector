package catalog

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/store"
)

// Suggestion is a near-miss pair Regroup declined to link; confirm one
// with Link.
type Suggestion = matcher.Suggestion

// Report summarises one Regroup.
type Report struct {
	Listings   int // active listings matched
	Products   int // products among them after the run
	MultiShop  int // products listed by two or more shops
	Created    int // new product rows
	Removed    int // product rows left with no listings
	Reassigned int // listings whose product changed
	// ByMethod counts listings per matcher.Method*; MethodNone is "alone".
	ByMethod map[string]int
	// Suggestions are the near-miss pairs, in the matcher's order (best
	// score first is up to the caller).
	Suggestions []Suggestion
}

// Regroup matches every active listing against every other and stores the
// result. A group keeps the product id its members already had (lowest
// wins; a split gets fresh ids), so ids stay stable between runs, and
// hand-made links are never undone.
//
// Inactive listings (no longer on their shop's site) are left where they
// are. They take no part in matching, so a removed listing can neither
// block its live replacement (one shop never lists a kit twice) nor name
// the product; they come back into play if the shop lists them again.
func (c *Catalog) Regroup(ctx context.Context) (Report, error) {
	var rep Report
	err := c.store.InProductTx(ctx, func(tx *store.ProductTx) error {
		rows, err := tx.Listings(ctx)
		if err != nil {
			return err
		}
		live := activeListings(rows)
		groups, suggestions := matcher.Match(live)
		plans, r := plan(live, groups)
		r.Suggestions = suggestions

		now := time.Now().UTC().Format(time.RFC3339)
		for _, p := range plans {
			pid := p.productID
			if pid == 0 {
				if pid, err = tx.CreateProduct(ctx, p.name, p.grade, now); err != nil {
					return err
				}
				r.Created++
			} else if err := tx.UpdateProduct(ctx, pid, p.name, p.grade); err != nil {
				return err
			}
			for _, m := range p.members {
				if err := tx.AssignSet(ctx, m.id, pid, m.method); err != nil {
					return err
				}
			}
		}
		if r.Removed, err = tx.PruneProducts(ctx); err != nil {
			return err
		}
		rep = r
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	return rep, nil
}

// Link puts listing b into listing a's product by hand. Both become
// manual, so Regroup leaves them alone from then on.
func (c *Catalog) Link(ctx context.Context, a, b int64) error {
	return c.store.InProductTx(ctx, func(tx *store.ProductTx) error {
		rows, err := tx.Listings(ctx)
		if err != nil {
			return err
		}
		ra, err := findListing(rows, a)
		if err != nil {
			return err
		}
		if _, err := findListing(rows, b); err != nil {
			return err
		}
		if ra.ProductID == 0 {
			return fmt.Errorf("set %d has no product yet — run `match` first", a)
		}
		for _, id := range []int64{a, b} {
			if err := tx.AssignSet(ctx, id, ra.ProductID, matcher.MethodManual); err != nil {
				return err
			}
		}
		_, err = tx.PruneProducts(ctx)
		return err
	})
}

// Unlink moves a listing into a product of its own and pins it there.
func (c *Catalog) Unlink(ctx context.Context, id int64) error {
	return c.store.InProductTx(ctx, func(tx *store.ProductTx) error {
		rows, err := tx.Listings(ctx)
		if err != nil {
			return err
		}
		r, err := findListing(rows, id)
		if err != nil {
			return err
		}
		pid, err := tx.CreateProduct(ctx, r.Name, r.Grade, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		if err := tx.AssignSet(ctx, id, pid, matcher.MethodManual); err != nil {
			return err
		}
		_, err = tx.PruneProducts(ctx)
		return err
	})
}

func findListing(rows []store.ListingRow, id int64) (store.ListingRow, error) {
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return store.ListingRow{}, fmt.Errorf("set %d: not found", id)
}

// toMatcher is the one place a stored listing becomes matcher input: a
// listing whose product was chosen by hand is Manual.
func toMatcher(r store.ListingRow) matcher.Listing {
	return matcher.Listing{ID: r.ID, Shop: r.Shop, Name: r.Name, Grade: r.Grade, EAN: r.EAN,
		ProductID: r.ProductID, Manual: r.Method == matcher.MethodManual}
}

// activeListings converts the rows still on sale.
func activeListings(rows []store.ListingRow) []matcher.Listing {
	var out []matcher.Listing
	for _, r := range rows {
		if r.Active {
			out = append(out, toMatcher(r))
		}
	}
	return out
}

// productPlan is the desired state of one product.
type productPlan struct {
	productID int64 // existing product to keep; 0 creates a new one
	name      string
	grade     string
	members   []planMember
}

type planMember struct {
	id     int64
	method string
}

// plan turns matcher groups into the products to store, and fills in the
// parts of the report that come from the groups themselves.
func plan(listings []matcher.Listing, groups []matcher.Group) ([]productPlan, Report) {
	byID := make(map[int64]matcher.Listing, len(listings))
	for _, l := range listings {
		byID[l.ID] = l
	}
	rep := Report{ByMethod: map[string]int{}, Listings: len(listings)}

	plans := make([]productPlan, 0, len(groups))
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
		p := productPlan{productID: pid, name: name, grade: grade}

		shops := map[string]bool{}
		for _, id := range g.Members {
			l := byID[id]
			shops[l.Shop] = true
			if pid == 0 || l.ProductID != pid {
				rep.Reassigned++
			}
			rep.ByMethod[g.Method[id]]++
			p.members = append(p.members, planMember{id: id, method: g.Method[id]})
		}
		rep.Products++
		if len(shops) > 1 {
			rep.MultiShop++
		}
		plans = append(plans, p)
	}
	return plans, rep
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
