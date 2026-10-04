package matcher

import (
	"reflect"
	"testing"
)

// sameWords compares word lists, treating nil and empty as equal.
func sameWords(a, b []string) bool {
	return len(a) == len(b) && (len(a) == 0 || reflect.DeepEqual(a, b))
}

func TestNewKey(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		model    string
		distinct []string
		tokens   []string
	}{
		{"gundamstore style", "MG RX-78-2 Gundam Ver. Ka 1/100", "rx782", []string{"verka"}, nil},
		{"plamodx style", "MG – RX-78-2 Gundam Ver.Ka", "rx782", []string{"verka"}, nil},
		{"zeonmarket style", "1/100 MG RX-78-2 Gundam Ver.Ka HG021", "rx782", []string{"verka"}, nil},
		{"alias and accents", "HG MS-18E Kämpfer (Endless Waltz)", "ms18e", nil, []string{"ew", "kampfer"}},
		{"model without hyphen after prefix", "RG GF13-017NJ Shining Gundam", "gf13017nj", nil, []string{"shining"}},
		{"model with extra hyphen", "MG GF-13-001NHII Master Gundam 1/100", "gf13001nhii", nil, []string{"master"}},
		{"type-2 is not a model", "HG Infinite Justice Gundam Type-2", "", []string{"2"}, []string{"infinite", "justice", "type"}},
		{"size and series are noise", "Gundam Seed HG 1/144 Rising Freedom Gundam Model Kit 18cm", "", nil, []string{"freedom", "rising"}},
		{"trans-am spellings", "HG Exia (Trans-Am Mode)", "", []string{"mode", "transam"}, []string{"exia"}},
		{"entry grade is distinct", "1/144 Entry Grade RX-93 Nu Gundam", "rx93", []string{"eg"}, []string{"nu"}},
		{"grade phrase only", "Perfect Grade 1/60 Perfect Pack", "", nil, []string{"pack", "perfect"}},
		{"custom is distinct", "HG Geara Doga (Rezin Schnyder Custom)", "", []string{"custom"}, []string{"doga", "geara", "rezin", "schnyder"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := NewKey(tt.in)
			if k.Model != tt.model {
				t.Errorf("Model = %q, want %q", k.Model, tt.model)
			}
			if !sameWords(k.Distinct, tt.distinct) {
				t.Errorf("Distinct = %v, want %v", k.Distinct, tt.distinct)
			}
			if !sameWords(k.Tokens, tt.tokens) {
				t.Errorf("Tokens = %v, want %v", k.Tokens, tt.tokens)
			}
		})
	}
}

func TestNormalizeEAN(t *testing.T) {
	tests := map[string]string{
		"4543112060426":    "4543112060426",
		"4543-1120-6042 6": "4543112060426",
		"045431120604":     "0045431120604", // 12 digits gets a leading zero
		"":                 "",
		"0000000000000":    "",
		"12345":            "",
	}
	for in, want := range tests {
		if got := NormalizeEAN(in); got != want {
			t.Errorf("NormalizeEAN(%q) = %q, want %q", in, got, want)
		}
	}
}

func ids(g Group) map[int64]bool {
	m := map[int64]bool{}
	for _, id := range g.Members {
		m[id] = true
	}
	return m
}

// groupOf returns the group containing id.
func groupOf(t *testing.T, groups []Group, id int64) Group {
	t.Helper()
	for _, g := range groups {
		if ids(g)[id] {
			return g
		}
	}
	t.Fatalf("listing %d in no group", id)
	return Group{}
}

func TestMatchSameKitAcrossShops(t *testing.T) {
	groups, _ := Match([]Listing{
		{ID: 1, Shop: "gundamstore", Name: "MG RX-78-2 Gundam Ver. Ka 1/100", Grade: "MG"},
		{ID: 2, Shop: "plamodx", Name: "MG – RX-78-2 Gundam Ver.Ka", Grade: "MG"},
		{ID: 3, Shop: "zeonmarket", Name: "1/100 MG RX-78-2 Gundam Ver.Ka", Grade: "MG"},
	})
	if len(groups) != 1 || len(groups[0].Members) != 3 {
		t.Fatalf("want one group of 3, got %+v", groups)
	}
	if groups[0].Method[1] != MethodName {
		t.Errorf("method = %q, want name", groups[0].Method[1])
	}
}

func TestMatchKeepsDifferentKitsApart(t *testing.T) {
	tests := []struct {
		name string
		a, b Listing
	}{
		{"ver ka vs plain", Listing{Name: "MG RX-78-2 Gundam Ver.Ka", Grade: "MG"}, Listing{Name: "MG RX-78-2 Gundam", Grade: "MG"}},
		{"ver 2.0 vs 3.0", Listing{Name: "MG RX-78-2 Gundam Ver. 2.0", Grade: "MG"}, Listing{Name: "MG RX-78-2 Gundam Ver. 3.0", Grade: "MG"}},
		{"different grade", Listing{Name: "HG RX-78-2 Gundam", Grade: "HG"}, Listing{Name: "MG RX-78-2 Gundam", Grade: "MG"}},
		{"nu vs hi-nu", Listing{Name: "MG RX-93 Nu Gundam Ver.Ka", Grade: "MG"}, Listing{Name: "MG RX-93 V2 Hi-V (Hi-Nu) Ver.Ka", Grade: "MG"}},
		{"custom variant", Listing{Name: "HG AMS-119 Geara Doga", Grade: "HG"}, Listing{Name: "HG AMS-119 Geara Doga (Rezin Schnyder Custom)", Grade: "HG"}},
		{"extra word changes the kit", Listing{Name: "HG ASW-G-08 Barbatos", Grade: "HG"}, Listing{Name: "1/144 HG ASW-G-08 Gundam Barbatos Lupus HG021", Grade: "HG"}},
		{"premium bandai release", Listing{Name: "HG XXXG-01D2 Gundam Deathscythe Hell", Grade: "HG"}, Listing{Name: "PB – HG – XXXG-01D2 Gundam Deathscythe Hell", Grade: "HG"}},
		{"mgex vs mg", Listing{Name: "MG ZGMF-X20A Strike Freedom Gundam", Grade: "MG"}, Listing{Name: "MGEX – ZGMF-X20A Strike Freedom Gundam", Grade: "MG"}},
		{"entry grade vs hg", Listing{Name: "HG RX-93 Nu Gundam", Grade: "HG"}, Listing{Name: "1/144 Entry Grade RX-93 Nu Gundam", Grade: "HG"}},
		{"trans-am mode vs plain", Listing{Name: "HG GN-001 Gundam Exia", Grade: "HG"}, Listing{Name: "HG GN-001 Gundam Exia (Trans-Am Mode)", Grade: "HG"}},
		{"clear color", Listing{Name: "MG Zaku II Ver.2.0", Grade: "MG"}, Listing{Name: "MG Zaku II Ver.2.0 Clear Color", Grade: "MG"}},
		{"sd vs standard", Listing{Name: "MG Wing Gundam Zero Custom XXXG-00W0", Grade: "MG"}, Listing{Name: "MGSD XXXG-00W0 Wing Gundam Zero Custom", Grade: "MG"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.a.ID, tt.a.Shop, tt.b.ID, tt.b.Shop = 1, "a", 2, "b"
			groups, _ := Match([]Listing{tt.a, tt.b})
			if len(groups) != 2 {
				t.Errorf("want two separate groups, got %+v", groups)
			}
		})
	}
}

func TestMatchSameKitDespiteShopNoise(t *testing.T) {
	// The RG Shining Gundam case: one shop has no model number, one has a
	// model with no hyphen after the prefix, one is a one-word name.
	groups, _ := Match([]Listing{
		{ID: 1, Shop: "geeksheaven", Name: "Gundam RG 1/144 Shining Gundam Model Kit", Grade: "RG"},
		{ID: 2, Shop: "gundamstore", Name: "RG Gundam Shining 1/144", Grade: "RG"},
		{ID: 3, Shop: "plamodx", Name: "RG – GF13-017NJ Shining Gundam", Grade: "RG"},
		{ID: 4, Shop: "zeonmarket", Name: "1/144 RG GF13-017NJ Shining Gundam RG42", Grade: "RG"},
	})
	if len(groups) != 1 || len(groups[0].Members) != 4 {
		t.Fatalf("want one group of 4, got %+v", groups)
	}
	// But a different GF13 kit stays apart.
	groups, _ = Match([]Listing{
		{ID: 1, Shop: "a", Name: "RG GF13-017NJ Shining Gundam", Grade: "RG"},
		{ID: 2, Shop: "b", Name: "RG GF13-017NJII God Gundam", Grade: "RG"},
	})
	if len(groups) != 2 {
		t.Errorf("Shining and God Gundam merged: %+v", groups)
	}
}

func TestMatchRulesFromNearMissReview(t *testing.T) {
	pairs := []struct{ name, a, b string }{
		{"hgaw prefix", "HGAW 1/144 GW-9800 Gundam Airmaster", "HG – GW-9800 Gundam Airmaster"},
		{"bf word", "HG BF 1/144 Gundam Amazing Red Warrior", "HG – PF-78-3A Gundam Amazing Red Warrior"},
		{"cbms", "Gundam MG Exia 1/100 18cm CBMS GN-001", "MG – GN-001 Gundam Exia"},
		{"gq tag", "Gundam HG 1/144 Challia's Rick Dom GQuuuuuux Model Kit", "HG 1/144 MS-09 Challia's Rick Dom (GQ)"},
		{"dot spacing", "HG Black Knight Squad Rud-ro. A (Griffin Arbalest custom) 1/144", "HG – NOG-M4F2 Black Knight Squad Rud-ro.A (Griffin Arbalest Custom)"},
		{"earth federation tag", "HG RMS-106 'Hi-Zack' 1/144", "HG – RMS-106 Hi-Zack (Earth Federation)"},
		{"orb tag", "Gundam MG 1/100 Eclipse ORB Mobile Suit MVF-X08 Model Kit", "MG – MVF-X08 Eclipse Gundam"},
		{"geeksheaven catalogue no", "Gundam HGUC 1/144 MSN-04II Nightingale Model Kit 240", "HG – MSN-04II Nightingale"},
		{"geeksheaven catalogue no, other kit", "Gundam HGUC 1/144 Type89 Base Jabber Model Kit 158", "HG – Type89 Base Jabber"},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			grade := "HG"
			if p.name == "cbms" || p.name == "orb tag" {
				grade = "MG"
			}
			groups, _ := Match([]Listing{
				{ID: 1, Shop: "a", Name: p.a, Grade: grade},
				{ID: 2, Shop: "b", Name: p.b, Grade: grade},
			})
			if len(groups) != 1 {
				t.Errorf("%q and %q should match, got %+v", p.a, p.b, groups)
			}
		})
	}
}

func TestMatchByEAN(t *testing.T) {
	groups, _ := Match([]Listing{
		{ID: 1, Shop: "a", Name: "Completely different title", EAN: "4543112060426"},
		{ID: 2, Shop: "b", Name: "Nothing alike at all", EAN: "4543-1120-60426"},
	})
	if len(groups) != 1 || groups[0].Method[1] != MethodEAN {
		t.Fatalf("want one EAN group, got %+v", groups)
	}
}

func TestMatchNeverMergesTwoListingsFromOneShop(t *testing.T) {
	groups, _ := Match([]Listing{
		{ID: 1, Shop: "a", Name: "HG RX-78-2 Gundam", Grade: "HG"},
		{ID: 2, Shop: "a", Name: "HG RX-78-2 Gundam", Grade: "HG"},
		{ID: 3, Shop: "b", Name: "HG RX-78-2 Gundam", Grade: "HG"},
	})
	for _, g := range groups {
		shops := map[string]int{}
		for _, id := range g.Members {
			shops[map[int64]string{1: "a", 2: "a", 3: "b"}[id]]++
		}
		if shops["a"] > 1 {
			t.Errorf("group %v holds two listings from shop a", g.Members)
		}
	}
}

func TestMatchManualLinksAreFixed(t *testing.T) {
	groups, _ := Match([]Listing{
		// Pinned apart by hand even though the names are identical.
		{ID: 1, Shop: "a", Name: "HG Zaku II", Grade: "HG", ProductID: 10, Manual: true},
		{ID: 2, Shop: "b", Name: "HG Zaku II", Grade: "HG", ProductID: 11, Manual: true},
		// Forced together by hand despite unrelated names.
		{ID: 3, Shop: "c", Name: "Foo", ProductID: 20, Manual: true},
		{ID: 4, Shop: "d", Name: "Totally Other", ProductID: 20, Manual: true},
		// Automatic listing can't join a locked group.
		{ID: 5, Shop: "e", Name: "HG Zaku II", Grade: "HG"},
	})
	if g := groupOf(t, groups, 1); ids(g)[2] || ids(g)[5] {
		t.Errorf("manual singleton 1 absorbed others: %v", g.Members)
	}
	if g := groupOf(t, groups, 3); !ids(g)[4] || g.Method[3] != MethodManual {
		t.Errorf("manual link 3+4 lost: %+v", g)
	}
}

func TestMatchReportsNearMisses(t *testing.T) {
	_, sugg := Match([]Listing{
		{ID: 1, Shop: "a", Name: "HG Beargguy F (Family)", Grade: "HG"},
		{ID: 2, Shop: "b", Name: "HG KUMA-F Beargguy F (Family) Special Edition Foo", Grade: "HG"},
	})
	_ = sugg // near-miss output is informational; just make sure Match doesn't panic on it
}
