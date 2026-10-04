package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/store"
)

// maxSuggestions caps the near-miss list printed by `match`.
const maxSuggestions = 15

// matchAll groups every listing into products and stores the result.
func matchAll(ctx context.Context, db *store.Store) (store.MatchStats, []matcher.Suggestion, error) {
	listings, err := db.MatchListings(ctx)
	if err != nil {
		return store.MatchStats{}, nil, err
	}
	groups, suggestions := matcher.Match(listings)
	stats, err := db.ApplyMatches(ctx, listings, groups, time.Now().UTC().Format(time.RFC3339))
	return stats, suggestions, err
}

// matchAfterCollect runs at the end of `collect`, so reports and the search
// UI always see products that include the listings just scraped. It is
// best-effort: a matching problem is logged but never turns a successful
// scrape into a failed collect run.
func matchAfterCollect(ctx context.Context, db *store.Store, logger *slog.Logger) {
	stats, suggestions, err := matchAll(ctx, db)
	if err != nil {
		logger.Error("match failed (listings are collected, but product grouping is stale)", "error", err)
		return
	}
	logger.Info("match complete", "listings", stats.ListingCount, "products", stats.Products,
		"multi_shop", stats.MultiShop, "reassigned", stats.Reassigned, "near_misses", len(suggestions))
}

// runMatch is the `match` command: group everything and print what it did —
// totals, then the near-miss pairs it declined to link so they can be
// confirmed with `link`.
func runMatch(ctx context.Context, db *store.Store, out io.Writer) error {
	stats, suggestions, err := matchAll(ctx, db)
	if err != nil {
		return err
	}

	var b strings.Builder // one write at the end, so there's a single error to check
	fmt.Fprintf(&b, "listings:            %d\n", stats.ListingCount)
	fmt.Fprintf(&b, "products:            %d (%d listed by 2+ shops)\n", stats.Products, stats.MultiShop)
	fmt.Fprintf(&b, "created / removed:   %d / %d products, %d listings reassigned\n", stats.Created, stats.Removed, stats.Reassigned)
	methods := make([]string, 0, len(stats.ByMethod))
	for m := range stats.ByMethod {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	for _, m := range methods {
		fmt.Fprintf(&b, "  %-8s %d listings\n", m, stats.ByMethod[m])
	}

	if len(suggestions) > 0 {
		sort.SliceStable(suggestions, func(i, j int) bool { return suggestions[i].Score > suggestions[j].Score })
		fmt.Fprintf(&b, "\nnot linked, but close (confirm with `link <a> <b>`):\n")
		for i, sg := range suggestions {
			if i == maxSuggestions {
				fmt.Fprintf(&b, "  ... and %d more\n", len(suggestions)-maxSuggestions)
				break
			}
			fmt.Fprintf(&b, "  %.2f  #%d [%s] %s\n        #%d [%s] %s\n", sg.Score, sg.A.ID, sg.A.Shop, sg.A.Name, sg.B.ID, sg.B.Shop, sg.B.Name)
		}
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// runLink handles `link <a> <b>` and `unlink <id>`.
func runLink(ctx context.Context, db *store.Store, cmd string, args []string) error {
	want := 2
	if cmd == "unlink" {
		want = 1
	}
	if len(args) != want {
		return fmt.Errorf("%s: expected %d set id(s), got %d", cmd, want, len(args))
	}
	ids := make([]int64, len(args))
	for i, a := range args {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: invalid set id %q", cmd, a)
		}
		ids[i] = id
	}
	if cmd == "unlink" {
		return db.UnlinkSet(ctx, ids[0], time.Now().UTC().Format(time.RFC3339))
	}
	return db.LinkSets(ctx, ids[0], ids[1])
}
