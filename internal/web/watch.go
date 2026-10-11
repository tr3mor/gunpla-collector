package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"gunpla-collector/internal/catalog"
	"gunpla-collector/internal/store"
)

// maxBody bounds a JSON request body.
const maxBody = 1 << 20

func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

// failWatch answers a failed watch operation.
func (s *Server) failWatch(w http.ResponseWriter, err error) {
	var bad *catalog.ErrInvalidWatch
	switch {
	case errors.As(err, &bad):
		s.writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrWatchNotFound):
		s.writeError(w, http.StatusNotFound, err)
	default:
		s.fail(w, err)
	}
}

type watchVariantJSON struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Selected  bool   `json:"selected"`
	Listings  int    `json:"listings"`
	HasBest   bool   `json:"has_best"`
	BestCents int    `json:"best_cents"`
	BestShop  string `json:"best_shop"`
}

type watchMatchJSON struct {
	searchResultJSON
	Variant  string `json:"variant"`
	Excluded bool   `json:"excluded"`
	Pinned   bool   `json:"pinned"`
	Best     bool   `json:"best"`
}

type watchPreviewJSON struct {
	Label           string             `json:"label,omitempty"`
	Spec            catalog.WatchSpec  `json:"spec"`
	Variants        []watchVariantJSON `json:"variants"`
	Matches         []watchMatchJSON   `json:"matches"`
	MissingVariants []string           `json:"missing_variants"`
}

func (s *Server) matchJSON(m catalog.WatchMatch) watchMatchJSON {
	return watchMatchJSON{searchResultJSON: s.toResultJSON(m.SetSearchRow), Variant: m.Variant, Excluded: m.Excluded, Pinned: m.Pinned, Best: m.Best}
}

func (s *Server) previewJSON(label string, p catalog.WatchPreview) watchPreviewJSON {
	out := watchPreviewJSON{
		Label: label, Spec: p.Spec, MissingVariants: nonNil(p.MissingVariants),
		Variants: make([]watchVariantJSON, 0, len(p.Variants)),
		Matches:  make([]watchMatchJSON, 0, len(p.Matches)),
	}
	for _, v := range p.Variants {
		out.Variants = append(out.Variants, watchVariantJSON{
			Key: v.Key, Label: v.Label, Selected: v.Selected, Listings: v.Listings,
			HasBest: v.HasBest, BestCents: v.BestCents, BestShop: v.BestShop,
		})
	}
	for _, m := range p.Matches {
		out.Matches = append(out.Matches, s.matchJSON(m))
	}
	return out
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// watchPreviewRequest asks what a watch resolves to: either a listing to
// derive the default "this kit" spec from, or an (edited) spec.
type watchPreviewRequest struct {
	SetID int64              `json:"set_id"`
	Spec  *catalog.WatchSpec `json:"spec"`
}

// handleWatchPreview resolves a watch spec against the catalog. It only
// reads, so it needs no write guard.
func (s *Server) handleWatchPreview(w http.ResponseWriter, r *http.Request) {
	var req watchPreviewRequest
	if !s.decode(w, r, &req) {
		return
	}
	var spec catalog.WatchSpec
	label := ""
	switch {
	case req.Spec != nil:
		spec = *req.Spec
	case req.SetID > 0:
		var err error
		if spec, label, err = s.catalog.SpecFromSet(r.Context(), req.SetID); err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
	default:
		s.writeError(w, http.StatusBadRequest, errors.New("set_id or spec is required"))
		return
	}
	p, err := s.catalog.PreviewWatch(r.Context(), spec)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, s.previewJSON(label, p))
}

// watchJSON is the wire shape of a saved watch.
type watchJSON struct {
	ID            int64             `json:"id"`
	Label         string            `json:"label"`
	Spec          catalog.WatchSpec `json:"spec"`
	MaxPriceCents int               `json:"max_price_cents"`
	OnRestock     bool              `json:"on_restock"`
	OnNew         bool              `json:"on_new"`
	Enabled       bool              `json:"enabled"`
}

func (j watchJSON) watch() catalog.Watch {
	return catalog.Watch{ID: j.ID, Label: j.Label, Spec: j.Spec, MaxPriceCents: j.MaxPriceCents, OnRestock: j.OnRestock, OnNew: j.OnNew, Enabled: j.Enabled}
}

func toWatchJSON(w catalog.Watch) watchJSON {
	return watchJSON{ID: w.ID, Label: w.Label, Spec: w.Spec, MaxPriceCents: w.MaxPriceCents, OnRestock: w.OnRestock, OnNew: w.OnNew, Enabled: w.Enabled}
}

// watchStatusJSON is a watch with what it currently resolves to.
type watchStatusJSON struct {
	watchJSON
	Hit             bool            `json:"hit"`
	Listings        int             `json:"listings"`
	Best            *watchMatchJSON `json:"best"`
	Versions        []string        `json:"versions"`
	MissingVariants []string        `json:"missing_variants"`
}

type watchesJSON struct {
	Watches []watchStatusJSON `json:"watches"`
	// WatchedSetIDs are the listings any enabled watch follows, so the
	// search page can mark them.
	WatchedSetIDs []int64 `json:"watched_set_ids"`
}

func (s *Server) handleWatches(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.catalog.WatchStatuses(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := watchesJSON{Watches: make([]watchStatusJSON, 0, len(statuses)), WatchedSetIDs: []int64{}}
	seen := map[int64]bool{}
	for _, st := range statuses {
		live := st.Preview.Live()
		j := watchStatusJSON{watchJSON: toWatchJSON(st.Watch), Hit: st.Hit, Listings: len(live), Versions: []string{}, MissingVariants: nonNil(st.Preview.MissingVariants)}
		for _, v := range st.Preview.Variants {
			if v.Selected {
				j.Versions = append(j.Versions, v.Label)
			}
		}
		if b, ok := st.Preview.Best(); ok {
			bj := s.matchJSON(b)
			j.Best = &bj
		}
		if st.Watch.Enabled {
			for _, m := range live {
				if !seen[m.SetID] {
					seen[m.SetID] = true
					out.WatchedSetIDs = append(out.WatchedSetIDs, m.SetID)
				}
			}
		}
		out.Watches = append(out.Watches, j)
	}
	s.writeJSON(w, out)
}

func (s *Server) handleWatchSave(w http.ResponseWriter, r *http.Request) {
	var in watchJSON
	if !s.decode(w, r, &in) {
		return
	}
	wt := in.watch()
	if r.Method == http.MethodPut {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			s.writeError(w, http.StatusBadRequest, errors.New("bad watch id"))
			return
		}
		wt.ID = id
	} else {
		wt.ID = 0
	}
	saved, err := s.catalog.SaveWatch(r.Context(), wt)
	if err != nil {
		s.failWatch(w, err)
		return
	}
	s.writeJSON(w, toWatchJSON(saved))
}

func (s *Server) handleWatchDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, errors.New("bad watch id"))
		return
	}
	if err := s.catalog.DeleteWatch(r.Context(), id); err != nil {
		s.failWatch(w, err)
		return
	}
	s.writeJSON(w, map[string]bool{"ok": true})
}
