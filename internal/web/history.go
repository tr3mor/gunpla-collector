package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"gunpla-collector/internal/store"
)

// historySeriesJSON is one listing's line on the price history chart.
type historySeriesJSON struct {
	SetID    int64              `json:"set_id"`
	ShopSlug string             `json:"shop_slug"`
	ShopName string             `json:"shop_name"`
	Name     string             `json:"name"`
	URL      string             `json:"url"`
	Active   bool               `json:"active"`
	Entries  []historyEntryJSON `json:"entries"`
}

// historyEntryJSON is one successful run of the listing's shop: a priced
// point, or (listed false) a run in which the shop did not list it.
type historyEntryJSON struct {
	At     string `json:"at"`
	Listed bool   `json:"listed"`
	// The rest is set only when listed. PriceCents is in EUR; the shop's own
	// price is beside it when it was not in EUR.
	Availability     string `json:"availability,omitempty"`
	PriceCents       int    `json:"price_cents,omitempty"`
	OriginalCents    int    `json:"original_price_cents,omitempty"`
	OriginalCurrency string `json:"original_currency,omitempty"`
}

// historyID reads a positive id from the query, reporting whether it was given.
func historyID(r *http.Request, key string) (id int64, given bool, err error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, false, nil
	}
	id, err = strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, true, fmt.Errorf("%s must be a positive number", key)
	}
	return id, true, nil
}

// handleHistory serves /api/history?product=ID (every listing of a kit) or
// /api/history?set=ID (one listing): the data behind the price chart.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	productID, hasProduct, err := historyID(r, "product")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	setID, hasSet, err := historyID(r, "set")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if hasProduct == hasSet {
		s.writeError(w, http.StatusBadRequest, errors.New("give exactly one of product or set"))
		return
	}

	series, err := s.catalog.History(r.Context(), productID, setID)
	if errors.Is(err, store.ErrKitNotFound) {
		s.writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	out := make([]historySeriesJSON, 0, len(series))
	for _, sr := range series {
		sj := historySeriesJSON{
			SetID: sr.SetID, ShopSlug: sr.ShopSlug, ShopName: sr.ShopName,
			Name: sr.Name, URL: sr.URL, Active: sr.Active,
			Entries: make([]historyEntryJSON, 0, len(sr.Entries)),
		}
		for _, e := range sr.Entries {
			ej := historyEntryJSON{At: e.At, Listed: e.Listed}
			if e.Listed {
				ej.Availability, ej.PriceCents = string(e.Availability), e.EURCents
				if e.Currency != "EUR" {
					ej.OriginalCents, ej.OriginalCurrency = e.Cents, e.Currency
				}
			}
			sj.Entries = append(sj.Entries, ej)
		}
		out = append(out, sj)
	}
	s.writeJSON(w, out)
}
