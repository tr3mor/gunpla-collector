package scraper

import (
	"regexp"
	"strings"
)

// nonKitPatterns match listings that sit in a shop's grade category but are
// not a Gunpla model kit: figures, display bases, parts-only add-ons, other
// product lines, a card game. Deliberately conservative — a real kit wrongly
// dropped is worse than an accessory kept — so each pattern is something
// seen in real shop data, and kit names that merely contain a similar word
// ("Type89 Base Jabber", "Perfect Pack Equipped", "Twin Set") are untouched.
var nonKitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)figure[- ]?rise`),              // Figure-Rise Standard/Labo/Effect: figures
	regexp.MustCompile(`(?i)\baction base\b`),              // display stands
	regexp.MustCompile(`(?i)\bdisplay (stand|base)\b`),     // third-party stands
	regexp.MustCompile(`(?i)\b30\s?mm\b`),                  // 30 Minutes Missions: a different line
	regexp.MustCompile(`(?i)^gundam assemble\b`),           // trading-card game
	regexp.MustCompile(`(?i)\bmetal build\b`),              // not plastic Gunpla
	regexp.MustCompile(`(?i)\bdecals?\b`),                  // decal sheets
	regexp.MustCompile(`(?i)\beffect (set|unit)\b`),        // effect parts only
	regexp.MustCompile(`(?i)\bparts only\b`),               // parts-only add-ons
	regexp.MustCompile(`(?i)\b(option|builders?) parts\b`), // Builders Parts
	regexp.MustCompile(`(?i)\bcustom parts set\b`),         // add-on parts
	regexp.MustCompile(`(?i)^sd\b|\bmgsd\b`),               // SD / MGSD: not MG/HG/RG/PG
	regexp.MustCompile(`(?i)\bweapons?\s*$`),               // "... Mercuone Weapons"
}

var (
	expansionRe = regexp.MustCompile(`(?i)\bexpansion\b`)
	// A kit sold together with its expansion ("Perfectibility + Divine
	// Expansion Set") is still a kit.
	kitBundleRe = regexp.MustCompile(`(?i)\bperfectibility\b`)
	// "Weapon Set 2" is an add-on; "OZ-06MS Leo (Full Weapon Set)" is a kit
	// that comes with extra weapons.
	weaponSetRe  = regexp.MustCompile(`(?i)\bweapon set\b`)
	fullWeaponRe = regexp.MustCompile(`(?i)\bfull weapon set\b`)
)

// IsKit reports whether a listing name is a model kit rather than an
// accessory, figure or other product line.
func IsKit(name string) bool {
	name = strings.TrimSpace(name)
	if expansionRe.MatchString(name) && !kitBundleRe.MatchString(name) {
		return false
	}
	if weaponSetRe.MatchString(name) && !fullWeaponRe.MatchString(name) {
		return false
	}
	for _, re := range nonKitPatterns {
		if re.MatchString(name) {
			return false
		}
	}
	return true
}

// FilterKits drops non-kit listings and returns the kept sets plus the
// names of those dropped.
func FilterKits(sets []ScrapedSet) (kits []ScrapedSet, dropped []string) {
	kits = make([]ScrapedSet, 0, len(sets))
	for _, s := range sets {
		if IsKit(s.Name) {
			kits = append(kits, s)
		} else {
			dropped = append(dropped, s.Name)
		}
	}
	return kits, dropped
}
