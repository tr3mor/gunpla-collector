package scraper

import "testing"

func TestIsKit(t *testing.T) {
	notKits := []string{
		"Figure-Rise Standard – Avatar Fumina",
		"Gundam Figure Rise Lunamaria Hawk Model Kit",
		"HG Figure Rise Standard - Fumina Hoshino",
		"Figure-rise Effect Jet Effect (Clear Blue)",
		"Action base 01 (clear)",
		"PG Action Base (Black)",
		"30MM – Customize Scene Base 06 (City Area Ver.)",
		"Gundam 30MM 1/144 Bexm-6 Roundnove II Model Kit",
		"Armored Core VI 30MM Weapon Set 06 Model Kit",
		"Gundam Assemble Expansion Pack 03 (EX03)",
		"Gundam Assemble Starter Set 01 (ST01)",
		"PB – MG – Expansion Parts Set for Gundam Barbatos Lupus",
		"PG 1/60 Banshee Expansion Pack [Armed Armor VN/BS]",
		"RG 1/144 Strike Freedom Gundam expansion effect unit \"Wings of the Skies\"",
		"RG 1/144 ν Gundam Fin Funnel Effect Set",
		"HG Amaim Warrior At The Borderline Weapon Set 2 1/72",
		"HG – PFF-X7/M1 Mercuone Weapons",
		"RG – Weapon Set for Evangelion",
		"RG Gundam Nu HWS Expansion Parts Only - P-Bandai 1/144",
		"Gundam Option Parts Set Gunpla 13 (Gunpla Batle Arm Arms) Model Kit",
		"HG 1/144 Zaku II F Type Chubs & Kale + Zaku II (Unidentified Type) Solari Custom Parts Set (RFV)",
		"Yamada – Connectable Display Base",
		"SNAP Toys – Smoke/LED Lights Display Stand + Remote Control  (Silver Base)",
		"Macross HG 1/100 YF-29 Durandal Valkyrie Water Decals Model Kit",
		"Moshow MCT-J02 Takeda Shingen Illustrious Class Metal Build",
		"SD – EX Valkylander",
		"MGSD NZ-666 Kshatriya",
		"MGSD – XXXG-00W0 Wing Gundam Zero Custom",
	}
	for _, n := range notKits {
		if IsKit(n) {
			t.Errorf("IsKit(%q) = true, want false", n)
		}
	}

	kits := []string{
		"HG Type89 Base Jabber 1/144",
		"Gundam HGUC 1/144 Type89 Base Jabber Model Kit 158",
		"HG Gundam G-Self (Perfect Pack Equipped) 1/144",
		"HG – YG-111 Gundam G-Self Atmospheric Pack",
		"HG RB-79 Ball Twin Set 1/144",
		"HG – MS-06 Zaku II (The Ground War Set)",
		"HG Tekkadan Complete Set - P-Bandai 1/144 *PREORDER*",
		"PG Unicorn Gundam Perfectibility + Divine Expansion Set 1/60",
		"PB – HG – OZ-06MS Leo (Full Weapon Set)",
		"HG Gundam Base Limited Zaku II (21st CENTURY REAL TYPE Ver.)",
		"HG – GBN-GF/RX78 GBN-Base Gundam",
		"RG – Evangelion Unit-00 (DX Positron Cannon Set)",
		"RG – Evangelion Unit-01 Test Type (DX Transport Stand Set)",
		"MG 1/100 Gundam Base Limited Freedom Gundam Ver.2.0 [Silver Coating]",
		"HG Mobile Doll May",
		"HG Gunpla Starter Set Vol.2 1/144",
		"MG RX-78-2 Gundam Ver. Ka 1/100",
		"HG – SD-237S Star Winning Gundam", // SD in a model number, not the SD line
		"HG Gundam Sandrock Custom",
	}
	for _, n := range kits {
		if !IsKit(n) {
			t.Errorf("IsKit(%q) = false, want true", n)
		}
	}
}

func TestFilterKits(t *testing.T) {
	in := []ScrapedSet{{Name: "MG RX-78-2"}, {Name: "Action Base 01"}, {Name: "HG Zaku"}}
	kits, dropped := FilterKits(in)
	if len(kits) != 2 || len(dropped) != 1 || dropped[0] != "Action Base 01" {
		t.Errorf("kits=%v dropped=%v", kits, dropped)
	}
}
