package matcher

import (
	"strings"
	"testing"
)

func review(ls ...Listing) []ReviewItem {
	groups, _ := Match(ls)
	return Review(ls, groups)
}

func TestReview_ExplainsABlockedLink(t *testing.T) {
	// Two PlamoDX listings share a name, so only one can join the
	// Gundam Store listing; the other must be explained, not silently lost.
	items := review(
		Listing{ID: 1, Shop: "gs", Name: "RG RX-0 Unicorn Gundam 1/144", Grade: "RG"},
		Listing{ID: 2, Shop: "pdx", Name: "RG – RX-0 Unicorn Gundam", Grade: "RG"},
		Listing{ID: 3, Shop: "pdx", Name: "RG – RX-0 Unicorn Gundam", Grade: "RG"},
	)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want the one pair left unlinked", items)
	}
	it := items[0]
	if it.Kind != KindClose || it.Blocker == "" || !strings.Contains(it.Reason, "already grouped with") {
		t.Errorf("item = %+v, want a close pair with a blocker", it)
	}
}

func TestReview_VersionDifferencesAndExclusions(t *testing.T) {
	items := review(
		Listing{ID: 1, Shop: "a", Name: "MG RX-78-2 Gundam Ver.Ka", Grade: "MG"},
		Listing{ID: 2, Shop: "b", Name: "MG RX-78-2 Gundam Ver.Ka Premium", Grade: "MG"}, // extra version word
		Listing{ID: 3, Shop: "b", Name: "HG RX-78-2 Gundam", Grade: "HG"},                // other grade
		Listing{ID: 4, Shop: "c", Name: "MG Wing Gundam Zero", Grade: "MG"},              // other suit
		Listing{ID: 5, Shop: "a", Name: "MG RX-78-2 Gundam Ver.Ka Premium", Grade: "MG"}, // same shop as 1
	)
	var versions int
	for _, it := range items {
		if it.Kind == KindVersion {
			versions++
		}
		if it.A.ID == 3 || it.B.ID == 3 || it.A.ID == 4 || it.B.ID == 4 {
			t.Errorf("pair %d/%d should not be offered: %+v", it.A.ID, it.B.ID, it)
		}
		if it.A.Shop == it.B.Shop {
			t.Errorf("same-shop pair offered: %+v", it)
		}
	}
	// 5 matches 2 exactly (a different shop, same words) and so isn't a
	// near miss; 1 vs 2 and 1 vs 5 differ by the Premium version word.
	if versions == 0 {
		t.Errorf("items = %+v, want the Premium version pairs", items)
	}
}

func TestReview_SkipsLinkedAndDifferentBarcodes(t *testing.T) {
	items := review(
		Listing{ID: 1, Shop: "a", Name: "MG RX-78-2 Gundam Ver.Ka", Grade: "MG", EAN: "4573102635324"},
		Listing{ID: 2, Shop: "b", Name: "MG RX-78-2 Gundam Ver.Ka Premium", Grade: "MG", EAN: "4573102635999"},
		Listing{ID: 3, Shop: "c", Name: "MG RX-78-2 Gundam Ver.Ka", Grade: "MG"},
	)
	for _, it := range items {
		if it.A.ID == 1 && it.B.ID == 2 {
			t.Errorf("pair with different barcodes offered: %+v", it)
		}
		if it.A.ID == 1 && it.B.ID == 3 {
			t.Errorf("an automatically linked pair offered: %+v", it)
		}
	}
}
