package matcher

import "testing"

func TestNewKey_ColonScaleIsNotAVersion(t *testing.T) {
	a := NewKey("Gundam HGUC Crossbone Gundam X1 Scale 1:144 Model Kit 187")
	b := NewKey("HG XM-X1 Crossbone Gundam X1 1/144")
	if !equal(a.Distinct, b.Distinct) {
		t.Errorf("Distinct %v vs %v, want equal (1:144 is a scale, not a version)", a.Distinct, b.Distinct)
	}
}

func TestSameSuit(t *testing.T) {
	key := NewKey
	cases := []struct {
		name        string
		spec, other string
		loose       bool
		want        bool
	}{
		{"model printed on one side only", "MG – FA-010S Full Armor ZZ Gundam Ver.Ka", "MG 1/100 Full Armor ZZ Gundam Ver.Ka", false, true},
		{"and the other way round", "MG 1/100 Full Armor ZZ Gundam Ver.Ka", "MG – FA-010S Full Armor ZZ Gundam Ver.Ka", false, true},
		{"both print different models", "MG FA-010S Full Armor ZZ Gundam", "MG FA-010A FAZZ", false, false},
		{"both print the same model, words agree", "MG RX-78-2 Gundam", "MG – RX-78-2 Gundam Origin", false, true},
		{"same model, wildly different words", "MG RX-78-2 Gundam Ver.Ka Revive Edition Alpha Beta", "MG RX-78-2 Gundam Origin Cucuruz Doan", false, false},
		{"one extra word on a short name is a different kit", "RG Nu Gundam", "RG Hi Nu Gundam", false, false},
		{"loose accepts extra words", "RG Nu Gundam", "RG Hi Nu Gundam", true, true},
		{"a missing word (zz) makes it another kit", "MG 1/100 Full Armor ZZ Gundam Ver.Ka", "MG – FA-78 Full Armor Gundam ver. Ka", false, false},
		{"one extra word on a longer name is fine", "MG Perfect Strike Freedom Gundam", "MG – Perfect Strike Freedom Gundam Special", false, true},
		{"unrelated", "MG Full Armor ZZ Gundam", "MG Wing Gundam Zero", false, false},
	}
	for _, tc := range cases {
		spec := key(tc.spec)
		if got := SameSuit(spec, key(tc.other), tc.loose); got != tc.want {
			t.Errorf("%s: SameSuit(%q, %q, loose=%v) = %v, want %v (keys %+v / %+v)", tc.name, tc.spec, tc.other, tc.loose, got, tc.want, spec, key(tc.other))
		}
	}
	if SameSuit(Key{}, key("MG Wing Gundam Zero"), false) {
		t.Error("an empty spec must match nothing")
	}
}
