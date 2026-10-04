package matcher

import (
	"sort"
	"strings"
)

// Match methods recorded per listing.
const (
	MethodEAN    = "ean"    // linked to another listing by identical barcode
	MethodName   = "name"   // linked by normalised-name similarity
	MethodManual = "manual" // set by hand (link/unlink); never changed automatically
	MethodNone   = ""       // alone in its group
)

const (
	// With the same model number, one name must be (almost) contained in
	// the other — shops add words like "Origin" — but not so lopsided that
	// a short name matches a long one ("Nu Gundam" vs "Hi-Nu Gundam").
	overlapAccept        = 0.75
	overlapJaccardAccept = 0.6
	// Without a shared model number the token sets must be near-identical.
	jaccardAccept = 0.8
	// Pairs scoring at least this but below the accept threshold are
	// reported as suggestions, never linked.
	suggestFloor = 0.5
)

// Listing is one shop's entry for a kit, as input to Match.
type Listing struct {
	ID    int64
	Shop  string
	Name  string
	Grade string
	EAN   string
	// ProductID and Manual carry hand-made links: listings that are Manual
	// and share a ProductID are forced into one group, and that group is
	// closed to automatic additions.
	ProductID int64
	Manual    bool
}

// Group is a set of listings judged to be the same kit.
type Group struct {
	Members []int64
	// Method[id] is how each member got into the group.
	Method map[int64]string
	Locked bool
}

// Suggestion is a near-miss pair that was not linked.
type Suggestion struct {
	A, B  Listing
	Score float64
}

type entry struct {
	Listing
	key Key
	ean string
}

type dsu struct {
	parent []int
	shops  []map[string]struct{}
	locked []bool
}

func newDSU(es []entry) *dsu {
	d := &dsu{parent: make([]int, len(es)), shops: make([]map[string]struct{}, len(es)), locked: make([]bool, len(es))}
	for i, e := range es {
		d.parent[i] = i
		d.shops[i] = map[string]struct{}{e.Shop: {}}
	}
	return d
}

func (d *dsu) find(i int) int {
	for d.parent[i] != i {
		d.parent[i] = d.parent[d.parent[i]]
		i = d.parent[i]
	}
	return i
}

// union merges the groups of a and b. Automatic merges (force=false) are
// refused if either group is locked or both already contain a listing from
// the same shop — one shop doesn't list the same kit twice, so that would
// be a bad merge, and refusing it stops fuzzy matches chaining unrelated
// kits into one blob.
func (d *dsu) union(a, b int, force bool) bool {
	ra, rb := d.find(a), d.find(b)
	if ra == rb {
		return true
	}
	if !force {
		if d.locked[ra] || d.locked[rb] {
			return false
		}
		for s := range d.shops[ra] {
			if _, clash := d.shops[rb][s]; clash {
				return false
			}
		}
	}
	d.parent[rb] = ra
	for s := range d.shops[rb] {
		d.shops[ra][s] = struct{}{}
	}
	d.locked[ra] = d.locked[ra] || d.locked[rb]
	return true
}

// Match groups listings that are the same kit. It also returns the
// near-miss pairs it declined to link, best first.
func Match(listings []Listing) ([]Group, []Suggestion) {
	es := make([]entry, len(listings))
	for i, l := range listings {
		es[i] = entry{Listing: l, key: NewKey(l.Name), ean: NormalizeEAN(l.EAN)}
	}
	d := newDSU(es)
	method := make([]string, len(es))

	// 1. Hand-made links are fixed and closed.
	firstManual := map[int64]int{}
	for i, e := range es {
		if !e.Manual {
			continue
		}
		method[i] = MethodManual
		d.locked[d.find(i)] = true
		if j, ok := firstManual[e.ProductID]; ok {
			d.union(j, i, true)
		} else {
			firstManual[e.ProductID] = i
		}
	}

	// 2. Identical barcodes are the same product.
	byEAN := map[string][]int{}
	for i, e := range es {
		if e.ean != "" && !e.Manual {
			byEAN[e.ean] = append(byEAN[e.ean], i)
		}
	}
	for _, idx := range byEAN {
		for _, j := range idx[1:] {
			if d.union(idx[0], j, false) {
				method[idx[0]], method[j] = MethodEAN, MethodEAN
			}
		}
	}

	// 3. Similar names, best score first.
	type pair struct {
		i, j   int
		score  float64
		accept bool
	}
	var pairs []pair
	for i := range es {
		for j := i + 1; j < len(es); j++ {
			if es[i].Shop == es[j].Shop || es[i].Manual || es[j].Manual {
				continue
			}
			if sc, ok := score(&es[i], &es[j]); sc >= suggestFloor {
				pairs = append(pairs, pair{i, j, sc, ok})
			}
		}
	}
	sort.Slice(pairs, func(a, b int) bool {
		if pairs[a].score != pairs[b].score {
			return pairs[a].score > pairs[b].score
		}
		if pairs[a].i != pairs[b].i {
			return pairs[a].i < pairs[b].i
		}
		return pairs[a].j < pairs[b].j
	})
	for _, p := range pairs {
		if !p.accept {
			continue
		}
		if d.union(p.i, p.j, false) {
			if method[p.i] == MethodNone {
				method[p.i] = MethodName
			}
			if method[p.j] == MethodNone {
				method[p.j] = MethodName
			}
		}
	}

	var suggestions []Suggestion
	seen := map[[2]int]bool{}
	for _, p := range pairs {
		ra, rb := d.find(p.i), d.find(p.j)
		if p.accept || ra == rb || seen[[2]int{ra, rb}] || d.locked[ra] || d.locked[rb] {
			continue
		}
		seen[[2]int{ra, rb}] = true
		suggestions = append(suggestions, Suggestion{A: es[p.i].Listing, B: es[p.j].Listing, Score: p.score})
	}

	byRoot := map[int]*Group{}
	var order []int
	for i, e := range es {
		r := d.find(i)
		g, ok := byRoot[r]
		if !ok {
			g = &Group{Method: map[int64]string{}, Locked: d.locked[r]}
			byRoot[r] = g
			order = append(order, r)
		}
		g.Members = append(g.Members, e.ID)
		g.Method[e.ID] = method[i]
	}
	groups := make([]Group, 0, len(order))
	for _, r := range order {
		groups = append(groups, *byRoot[r])
	}
	return groups, suggestions
}

// score rates how likely two listings are the same kit (0..1) and whether
// that is high enough to link them automatically.
func score(a, b *entry) (float64, bool) {
	if a.Grade != "" && b.Grade != "" && !strings.EqualFold(a.Grade, b.Grade) {
		return 0, false
	}
	if !equal(a.key.Distinct, b.key.Distinct) {
		return 0, false
	}
	ka, kb := a.key, b.key
	if ka.Model != "" && kb.Model != "" {
		if ka.Model != kb.Model {
			return 0, false
		}
		switch {
		case len(ka.Tokens) == 0 && len(kb.Tokens) == 0:
			return 1, true
		case len(ka.Tokens) == 0 || len(kb.Tokens) == 0:
			return suggestFloor, false // one name has extra words we can't judge
		}
		o, j := overlap(ka.Tokens, kb.Tokens), jaccard(ka.Tokens, kb.Tokens)
		return (o + j) / 2, o >= overlapAccept && j >= overlapJaccardAccept
	}
	oneModel := (ka.Model != "") != (kb.Model != "")
	// A model number on one side makes even a one-word name ("Shining")
	// specific enough, provided the grade is known to agree.
	minTokens := 2
	if oneModel && a.Grade != "" && strings.EqualFold(a.Grade, b.Grade) {
		minTokens = 1
	}
	if len(ka.Tokens) < minTokens || len(kb.Tokens) < minTokens {
		return 0, false
	}
	s := jaccard(ka.Tokens, kb.Tokens)
	return s, s >= jaccardAccept
}

func intersect(a, b []string) int {
	n, i, j := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			n++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return n
}

func jaccard(a, b []string) float64 {
	n := intersect(a, b)
	return float64(n) / float64(len(a)+len(b)-n)
}

func overlap(a, b []string) float64 {
	n := intersect(a, b)
	m := len(a)
	if len(b) < m {
		m = len(b)
	}
	return float64(n) / float64(m)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
