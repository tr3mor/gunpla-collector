package matcher

import (
	"sort"
	"strings"
)

// Review kinds.
const (
	// KindClose: same version words, but the names weren't similar enough to
	// link automatically.
	KindClose = "close"
	// KindVersion: the same suit with version words on one side that the
	// other lacks (e.g. "Premium", "Clear"). Often two real kits, sometimes
	// a shop dropping a word.
	KindVersion = "version"
)

// ReviewItem is a cross-shop pair of listings that were not grouped but
// might be the same kit, with enough detail to see why.
type ReviewItem struct {
	A, B   Listing
	KeyA   Key
	KeyB   Key
	Kind   string
	Score  float64
	Reason string
	// Blocker is set when the pair is similar enough to link but wasn't: a
	// listing already grouped with one of them comes from the other's shop
	// (a shop never lists a kit twice), so the merge was refused. It is that
	// listing's name and shop.
	Blocker string
	// OnlyA, OnlyB and Shared are the descriptive words (and version words)
	// that tell the two names apart, in matcher terms: the candidates for an
	// alias or noise-word entry in normalize.go.
	OnlyA, OnlyB, Shared []string
}

// Review lists the pairs worth a human look. groups is Match's result for
// the same listings: pairs already in one group, or touching a hand-made
// link, are skipped.
func Review(listings []Listing, groups []Group) []ReviewItem {
	group := map[int64]int{}
	for gi, g := range groups {
		for _, id := range g.Members {
			group[id] = gi
		}
	}
	es := make([]entry, len(listings))
	for i, l := range listings {
		es[i] = entry{Listing: l, key: NewKey(l.Name), ean: NormalizeEAN(l.EAN)}
	}

	members := make([][]int, len(groups))
	for i, e := range es {
		members[group[e.ID]] = append(members[group[e.ID]], i)
	}
	// blocker finds a listing in a's group from b's shop, or vice versa.
	blocker := func(a, b *entry) string {
		for _, pair := range [][2]*entry{{a, b}, {b, a}} {
			for _, m := range members[group[pair[0].ID]] {
				if es[m].Shop == pair[1].Shop {
					return es[m].Name + " [" + es[m].Shop + "]"
				}
			}
		}
		return ""
	}

	var out []ReviewItem
	for i := range es {
		for j := i + 1; j < len(es); j++ {
			a, b := &es[i], &es[j]
			if a.Shop == b.Shop || a.Manual || b.Manual || group[a.ID] == group[b.ID] {
				continue
			}
			if a.ean != "" && b.ean != "" && a.ean != b.ean {
				continue // different barcodes: known to be different kits
			}
			if a.Grade != "" && b.Grade != "" && !strings.EqualFold(a.Grade, b.Grade) {
				continue
			}
			item := ReviewItem{A: a.Listing, B: b.Listing, KeyA: a.key, KeyB: b.key}
			sc, accepted, why := judge(a, b)
			switch {
			case sc >= suggestFloor:
				item.Kind, item.Score, item.Reason = KindClose, sc, why
				if accepted {
					if item.Blocker = blocker(a, b); item.Blocker != "" {
						item.Reason += ", but not linked: already grouped with " + item.Blocker
					}
				}
			case !equal(a.key.Distinct, b.key.Distinct) && versionSubset(a.key.Distinct, b.key.Distinct) && sameSuitScore(a.key, b.key) >= suggestFloor:
				item.Kind, item.Score, item.Reason = KindVersion, sameSuitScore(a.key, b.key), why
			default:
				continue
			}
			item.OnlyA, item.OnlyB, item.Shared = diffWords(a.key, b.key)
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == KindClose
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// versionSubset reports whether one version-word set contains the other.
func versionSubset(a, b []string) bool {
	n := intersect(a, b)
	return n == len(a) || n == len(b)
}

// sameSuitScore rates how alike two names are apart from their version
// words: 1 for the same model number with no other words, otherwise the
// word similarity (0 for different model numbers).
func sameSuitScore(a, b Key) float64 {
	if a.Model != "" && b.Model != "" {
		if !sameModel(a.Model, b.Model) {
			return 0
		}
		if len(a.Tokens) == 0 && len(b.Tokens) == 0 {
			return 1
		}
		if len(a.Tokens) == 0 || len(b.Tokens) == 0 {
			return suggestFloor
		}
		return (overlap(a.Tokens, b.Tokens) + jaccard(a.Tokens, b.Tokens)) / 2
	}
	if len(a.Tokens) == 0 || len(b.Tokens) == 0 {
		return 0
	}
	return jaccard(a.Tokens, b.Tokens)
}

func diffWords(a, b Key) (onlyA, onlyB, shared []string) {
	count := map[string]int{}
	for _, w := range append(append([]string{}, a.Distinct...), a.Tokens...) {
		count[w] |= 1
	}
	for _, w := range append(append([]string{}, b.Distinct...), b.Tokens...) {
		count[w] |= 2
	}
	for w, c := range count {
		switch c {
		case 1:
			onlyA = append(onlyA, w)
		case 2:
			onlyB = append(onlyB, w)
		default:
			shared = append(shared, w)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	sort.Strings(shared)
	return
}
