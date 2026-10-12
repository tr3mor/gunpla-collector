package catalog

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// BaseVariant is the variant key of a kit with no distinguishing words
// (no Ver.Ka, Clear, Premium, ...): the plain release.
const BaseVariant = "~base"

// Limits on a stored spec, so a bad client can't make a watch expensive to
// evaluate or to store.
const (
	maxSpecWords = 30
	maxSpecPins  = 500
	maxWordLen   = 60
)

// WatchSpec describes which listings a watch follows. It is expressed in
// the matcher's facets (grade, model number, words, version) rather than as
// a name query, because one suit has many kits (MG 2.0, Ver.Ka, Clear, ...)
// and a name query can't tell them apart.
type WatchSpec struct {
	// Grade is compared case-insensitively; a listing with no grade (many
	// pre-orders) is compatible with any.
	Grade string `json:"grade"`
	// Model is a normalised model number ("rx782"). It is compared only with
	// listings that also name one: shops often leave it out, and those are
	// matched on Tokens instead.
	Model  string   `json:"model"`
	Tokens []string `json:"tokens"`
	// Loose makes Tokens a minimum (every one must appear in the listing)
	// instead of a near-exact match. Set when the user trims words.
	Loose bool `json:"loose"`
	// Variants are the version keys (see VariantKey) the watch accepts; empty
	// accepts all.
	Variants []string `json:"variants"`
	// Include and Exclude are per-listing overrides that win over the facets.
	Include []int64 `json:"include"`
	Exclude []int64 `json:"exclude"`
}

// normalized returns s with nil slices made empty (clean JSON) and words
// lower-cased and de-duplicated.
func (s WatchSpec) normalized() WatchSpec {
	s.Grade = strings.ToUpper(strings.TrimSpace(s.Grade))
	s.Model = strings.ToLower(strings.TrimSpace(s.Model))
	clean := func(in []string, fold bool) []string {
		out := []string{}
		for _, w := range in {
			w = strings.TrimSpace(w)
			if fold {
				w = strings.ToLower(w)
			}
			if w != "" && !slices.Contains(out, w) {
				out = append(out, w)
			}
		}
		return out
	}
	s.Tokens = clean(s.Tokens, true)
	s.Variants = clean(s.Variants, false)
	if s.Include == nil {
		s.Include = []int64{}
	}
	if s.Exclude == nil {
		s.Exclude = []int64{}
	}
	return s
}

// Validate reports why a spec can't be saved.
func (s WatchSpec) Validate() error {
	if s.Model == "" && len(s.Tokens) == 0 && len(s.Include) == 0 {
		return fmt.Errorf("a watch needs a model number, at least one word, or a pinned listing")
	}
	if len(s.Tokens) > maxSpecWords || len(s.Variants) > maxSpecWords || len(s.Include) > maxSpecPins || len(s.Exclude) > maxSpecPins {
		return fmt.Errorf("watch is too large")
	}
	for _, w := range append(append([]string{s.Model, s.Grade}, s.Tokens...), s.Variants...) {
		if len(w) > maxWordLen {
			return fmt.Errorf("%q is too long", truncateRunes(w, 20))
		}
	}
	return nil
}

// VariantKey names a listing's variant: its distinguishing words joined by
// "+", or BaseVariant when it has none.
func VariantKey(k matcher.Key) string {
	if len(k.Distinct) == 0 {
		return BaseVariant
	}
	return strings.Join(k.Distinct, "+")
}

// Variant is one flavour of the watched suit and how many listings it has.
type Variant struct {
	Key      string
	Label    string
	Selected bool
	Listings int
	// BestCents/BestShop describe the cheapest buyable listing in EUR;
	// HasBest is false when there is none (all out of stock).
	HasBest   bool
	BestCents int
	BestShop  string
}

// WatchMatch is a listing a watch follows (or, if Excluded, a listing of a
// selected variant the user ruled out).
type WatchMatch struct {
	store.SetSearchRow
	Variant  string
	Excluded bool
	// Pinned marks a listing added by hand rather than matched by the facets.
	Pinned bool
	// Best marks the cheapest buyable, non-excluded match.
	Best bool
}

// Buyable reports whether the listing can be bought now and is priced.
func (m WatchMatch) Buyable() bool {
	return m.HasEUR && m.Availability != scraper.AvailabilityOutOfStock
}

// WatchPreview is what a spec currently resolves to.
type WatchPreview struct {
	Spec     WatchSpec
	Variants []Variant
	Matches  []WatchMatch
	// MissingVariants are selected versions no listing has any more (a shop
	// renamed it, or the matcher's reading of the name changed).
	MissingVariants []string
}

// Best returns the cheapest buyable, non-excluded match.
func (p WatchPreview) Best() (WatchMatch, bool) {
	for _, m := range p.Matches {
		if m.Best {
			return m, true
		}
	}
	return WatchMatch{}, false
}

// Live returns the matches that are not excluded.
func (p WatchPreview) Live() []WatchMatch {
	var out []WatchMatch
	for _, m := range p.Matches {
		if !m.Excluded {
			out = append(out, m)
		}
	}
	return out
}

// snapshot is the active catalog with matcher keys, loaded once so many
// watches can be resolved without re-reading or re-normalising it.
type snapshot struct {
	rows []store.SetSearchRow
	keys []matcher.Key
}

func (c *Catalog) snapshot(ctx context.Context) (*snapshot, error) {
	rows, err := c.store.SearchSets(ctx, "")
	if err != nil {
		return nil, err
	}
	snap := &snapshot{rows: rows, keys: make([]matcher.Key, len(rows))}
	for i, r := range rows {
		snap.keys[i] = c.key(r.Name)
	}
	return snap, nil
}

func (c *Catalog) key(name string) matcher.Key {
	if k, ok := c.keys.Load(name); ok {
		return k.(matcher.Key)
	}
	k := matcher.NewKey(name)
	c.keys.Store(name, k)
	return k
}

// SpecFromSet derives the default spec for "watch this kit" from a listing:
// same grade, same suit, same version. The label is the listing's product
// (or listing) name.
func (c *Catalog) SpecFromSet(ctx context.Context, setID int64) (WatchSpec, string, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return WatchSpec{}, "", err
	}
	for i, row := range snap.rows {
		if row.SetID != setID {
			continue
		}
		k := snap.keys[i]
		spec := WatchSpec{Grade: row.Grade, Model: k.Model, Tokens: k.Tokens, Variants: []string{VariantKey(k)}}.normalized()
		// A name with no model number and no distinctive words can't match
		// anything, not even itself; pin it so the watch still works.
		if !spec.sameSuit(row, k) {
			spec.Include = append(spec.Include, setID)
		}
		label := row.ProductName
		if label == "" {
			label = row.Name
		}
		return spec, label, nil
	}
	return WatchSpec{}, "", fmt.Errorf("set %d not found", setID)
}

// PreviewWatch resolves spec against the current catalog: every variant of
// the suit that exists, and the listings of the selected variants.
func (c *Catalog) PreviewWatch(ctx context.Context, spec WatchSpec) (WatchPreview, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return WatchPreview{}, err
	}
	return snap.preview(spec.normalized()), nil
}

func (snap *snapshot) preview(spec WatchSpec) WatchPreview {
	byVariant := map[string]*Variant{}
	var matches []WatchMatch
	for i, row := range snap.rows {
		k := snap.keys[i]
		pinned := slices.Contains(spec.Include, row.SetID)
		suit := spec.sameSuit(row, k)
		if !suit && !pinned {
			continue
		}
		vk := VariantKey(k)
		selected := len(spec.Variants) == 0 || slices.Contains(spec.Variants, vk)
		if suit {
			v, ok := byVariant[vk]
			if !ok {
				v = &Variant{Key: vk, Label: variantLabel(k), Selected: selected}
				byVariant[vk] = v
			}
			v.Listings++
			if cents, ok := buyableEUR(row); ok && (!v.HasBest || cents < v.BestCents) {
				v.HasBest, v.BestCents, v.BestShop = true, cents, row.ShopName
			}
		}
		if (suit && selected) || pinned {
			matches = append(matches, WatchMatch{
				SetSearchRow: row, Variant: vk, Pinned: pinned,
				Excluded: slices.Contains(spec.Exclude, row.SetID),
			})
		}
	}

	// Cheapest first (unpriced last), excluded listings after the live ones.
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Excluded != b.Excluded {
			return !a.Excluded
		}
		if a.HasEUR != b.HasEUR {
			return a.HasEUR
		}
		return a.EURCents < b.EURCents
	})
	best := -1
	for i, m := range matches {
		if !m.Excluded && m.Buyable() && (best < 0 || m.EURCents < matches[best].EURCents) {
			best = i
		}
	}
	if best >= 0 {
		for i, m := range matches {
			if !m.Excluded && m.Buyable() && m.EURCents == matches[best].EURCents {
				matches[i].Best = true
			}
		}
	}

	variants := make([]Variant, 0, len(byVariant))
	for _, v := range byVariant {
		variants = append(variants, *v)
	}
	sort.Slice(variants, func(i, j int) bool {
		if (variants[i].Key == BaseVariant) != (variants[j].Key == BaseVariant) {
			return variants[i].Key == BaseVariant
		}
		return variants[i].Key < variants[j].Key
	})
	var missing []string
	for _, v := range spec.Variants {
		if _, ok := byVariant[v]; !ok {
			missing = append(missing, v)
		}
	}
	return WatchPreview{Spec: spec, Variants: variants, Matches: matches, MissingVariants: missing}
}

// sameSuit reports whether a listing is the spec's grade and suit,
// whatever its variant.
func (s WatchSpec) sameSuit(row store.SetSearchRow, k matcher.Key) bool {
	if s.Grade != "" && row.Grade != "" && !strings.EqualFold(row.Grade, s.Grade) {
		return false
	}
	return matcher.SameSuit(matcher.Key{Model: s.Model, Tokens: s.Tokens}, k, s.Loose)
}

// variantLabel renders a listing's version words for people: "Ver.Ka",
// "Clear + Ver.3.0", or "Base release" when it has none.
func variantLabel(k matcher.Key) string {
	if len(k.Distinct) == 0 {
		return "Base release"
	}
	words := make([]string, len(k.Distinct))
	for i, d := range k.Distinct {
		switch {
		case strings.HasPrefix(d, "ver") && len(d) > 3:
			words[i] = "Ver." + strings.ToUpper(d[3:4]) + d[4:]
		case d != "":
			words[i] = strings.ToUpper(d[:1]) + d[1:]
		}
	}
	return strings.Join(words, " + ")
}

// truncateRunes shortens s to n runes (not bytes, so a multi-byte character
// is never cut in half), marking the cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
