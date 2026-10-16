// Package matcher decides which shop listings are the same physical kit.
// It is pure logic (no database, no network): the store package feeds it
// listings and persists the groups it returns.
package matcher

import (
	"regexp"
	"sort"
	"strings"
)

// Key is a listing name reduced to the parts that identify a kit, so that
// "MG 1/100 XXXG-00W0 Wing Gundam Zero (Endless Waltz) Ver.Ka" and
// "Master Grade Wing Gundam Zero EW Ver. Ka" compare equal.
type Key struct {
	// Model is the mobile suit's model number with punctuation removed
	// ("RX-78-2" -> "rx782"), or "" if the name has none.
	Model string
	// Distinct are tokens that make two otherwise similar names different
	// products (Ver.Ka, Clear Color, Revive, 2.0, ...). Two listings can
	// only match if their Distinct sets are identical.
	Distinct []string
	// Tokens are the remaining descriptive words, sorted and de-duplicated.
	Tokens []string
	// Series is the Real Grade series number some shops print ("Model Kit
	// 35" at GeeksHeaven, "RG35" at ZeonMarket), or "". It identifies one
	// RG kit exactly, whatever the rest of the name says.
	Series string
}

// modelPattern matches one model number (see reModel).
const modelPattern = `[a-z]{1,5}-?[a-z]?\d{1,4}[a-z0-9]{0,8}(?:-[a-z0-9]{1,8})*(?:/[a-z0-9]{1,3})?|[a-z]{2,5}-[a-z]-\d{1,3}[a-z]?`

var (
	reScale = regexp.MustCompile(`\b(?:1|re)\s*[/:]\s*\d+\b`)
	reVer   = regexp.MustCompile(`\bver(?:sion)?\b\.?\s*([a-z0-9][a-z0-9.]*)`)
	reMk    = regexp.MustCompile(`\bmk[\s.\-]*(iii|ii|iv|v|[0-9])\b`)
	// A model number: 1-5 letters, optional hyphen, a digit run, then any
	// hyphen-separated alphanumeric tail ("RX-78-2", "GF13-017NJ",
	// "GF-13-001NHII", "XXXG-00W0", "ASW-G-08A", "ZGMF-X20A/B").
	reModel = regexp.MustCompile(`\b(` + modelPattern + `)(?:\b|$)`)
	// Two codes joined by "+" ("GN-0000+GNR-010"): keep the first.
	reJoinedCode = regexp.MustCompile(`\b(` + modelPattern + `)\s*\+\s*(?:` + modelPattern + `)(?:\b|$)`)
	reSplit      = regexp.MustCompile(`[^a-z0-9.]+`)
	// Shop-added dimensions ("18cm"), and the "GQuuuuuuX" series name in
	// all its spellings.
	reSize = regexp.MustCompile(`\b\d+\s?cm\b`)
	// GeeksHeaven appends its own catalogue number after the product type
	// ("... Model Kit 240"); a bare number would otherwise read as a
	// version marker.
	reModelKitNo = regexp.MustCompile(`\bmodel\s+kit\s+\d{1,3}\b`)
	reGQuux      = regexp.MustCompile(`\bgq?u{2,}x\b`)
	// "*NEW MODEL*" marks a re-tooled kit sold beside the old one; unlike a
	// stray "new" it makes a different product.
	reNewModel = regexp.MustCompile(`\*\s*new\s+model\s*\*`)
	// "+AQM/E-X01" after a Strike's model number names the pack, not a word.
	reStrikePack = regexp.MustCompile(`\+\s*[a-z]{2,5}/[a-z]-[a-z0-9]+`)
	// "Age II" and "AGEII", "Age 2" and "AGE-2" are the same name.
	reAgeII = regexp.MustCompile(`\bage\s+(ii|iii)\b`)
	reAgeN  = regexp.MustCompile(`\bage\s+(\d)\b`)
	// RG series numbers: "Model Kit 35" (with an RG in the name) or "RG35".
	reRGWord  = regexp.MustCompile(`\brg\b`)
	reRGSerie = regexp.MustCompile(`\brg(\d{1,3})\b`)
	reKitNo   = regexp.MustCompile(`\bmodel\s+kit\s+(\d{1,3})\b`)
	reEntry   = regexp.MustCompile(`\bentry\s+grade\b`)
	reTransAm = regexp.MustCompile(`\btrans[\s-]?am\b`)
	// "Master Grade" etc. are removed as a phrase, not word by word, so
	// "Perfect Pack" and "High Maneuver" keep their meaning.
	reGradeWords = regexp.MustCompile(`\b(?:master|high|real|perfect|no)\s+grade\b`)
	reRoman      = map[string]string{"ii": "2", "iii": "3", "iv": "4", "v": "5"}
)

// aliases rewrite phrases that different shops spell differently. Applied
// to the lowercased name before tokenising. Extend as new mismatches show
// up in `gunpla-collector match` output.
var aliases = []struct{ from, to string }{
	{"endless waltz", "ew"},
	{"p-bandai", "premium"},
	{"premium bandai", "premium"},
	{"pre-order", " "},
	{"preorder", " "},
	{"&", " and "},
}

// modelPrefixStop are letter prefixes that look like the start of a model
// number ("Type-2", "Ver-2") but aren't.
var modelPrefixStop = toSet("type", "ver", "mk", "no", "hi", "ex", "mg", "hg", "rg", "pg", "sd", "eg")

// noise words carry no identifying information: grade names (the grade is
// compared separately), packaging words, and words so common in Gunpla
// names that shops randomly include or omit them.
var noise = toSet(
	"grade", "mg", "hg", "rg", "pg",
	"hguc", "hgce", "hgac", "hgfc", "hgbf", "hgbd", "hgbdr", "hgto", "hgibo", "hgcc", "hggto",
	"bandai", "spirits", "gunpla", "gundam", "model", "kit", "plastic", "scale", "mobile", "suit",
	"the", "of", "and", "a", "with", "new", "ver", "version", "uc", "bandai",
	// Where a shop prints the franchise/series, or that a kit is an
	// overseas release, it is not part of the kit's identity.
	"seed", "destiny", "ibo", "wataru", "oversea", "overseas", "global",
	// Franchise/faction tags and shop-specific leftovers that one shop
	// prints and another drops: "(GQ)", "(Earth Federation)", "ORB", "BF",
	// GeeksHeaven's "CBMS" and "A Tentative".
	"gq", "earth", "federation", "orb", "bf", "cbms", "tentative", "fighters",
	// ZeonMarket prefixes some listings with its "Solomon" label, and one
	// shop marks a kit "Damaged Box".
	"solomon", "damaged", "box",
)

// reHGPrefix matches grade-prefix words like HGUC, HGAW, HGBF, HGIBO: the
// grade is compared separately, so these carry no information.
var reHGPrefix = regexp.MustCompile(`^hg[a-z]{2,5}$`)

// reDotBetweenLetters splits "rud-ro.a" / "d.e.e.p" so spacing after a dot
// ("Rud-ro. A" vs "Rud-ro.A") doesn't matter. Run twice to catch
// overlapping single-letter runs.
var reDotBetweenLetters = regexp.MustCompile(`([a-z])\.([a-z])`)

// distinct words that turn a kit into a different product.
var distinctWords = toSet(
	"clear", "metallic", "titanium", "finish", "gold", "ex", "revive", "limited",
	"special", "premium", "extra", "color", "colour", "expo", "event", "coating",
	"custom", "sd", "mgsd", "mgex", "eg", "mode", "transam", "newmodel",
)

// reCatalogNo matches shop catalogue numbers like "HG021" or "RG09" that
// only one shop puts in the title.
var reCatalogNo = regexp.MustCompile(`^(?:hg|rg|mg|pg)\d+$`)

// folder drops apostrophes and folds the accented/Greek letters shops use
// ("Kämpfer", "ν Gundam") to plain ASCII so they tokenise as one word.
var folder = strings.NewReplacer(
	"’", "", "'", "", "ä", "a", "ö", "o", "ü", "u", "é", "e", "è", "e",
	"ā", "a", "ō", "o", "ū", "u", "ν", "nu", "ζ", "z", "ω", "omega", "α", "a",
)

func toSet(words ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}

// NewKey normalises a listing name.
func NewKey(name string) Key {
	s := strings.ToLower(name)
	s = folder.Replace(s)
	var k Key
	k.Series = seriesNumber(s)
	s = reGradeWords.ReplaceAllString(s, " ")
	s = reNewModel.ReplaceAllString(s, " newmodel ")
	s = reStrikePack.ReplaceAllString(s, " ")
	s = reAgeII.ReplaceAllString(s, "age$1")
	s = reAgeN.ReplaceAllString(s, "age-$1")
	s = reEntry.ReplaceAllString(s, " eg ")
	s = reTransAm.ReplaceAllString(s, " transam ")
	s = reSize.ReplaceAllString(s, " ")
	s = reModelKitNo.ReplaceAllString(s, " ")
	s = reGQuux.ReplaceAllString(s, " ")
	for _, a := range aliases {
		s = strings.ReplaceAll(s, a.from, a.to)
	}
	s = reScale.ReplaceAllString(s, " ")
	s = reMk.ReplaceAllStringFunc(s, func(m string) string {
		n := reMk.FindStringSubmatch(m)[1]
		if arabic, ok := reRoman[n]; ok {
			n = arabic
		}
		return " mk" + n + " "
	})
	s = reVer.ReplaceAllString(s, " ver${1} ")
	for i := 0; i < 2; i++ {
		s = reDotBetweenLetters.ReplaceAllString(s, "$1 $2")
	}

	// The first model number identifies the suit. A second code joined to
	// it with "+" ("GN-0000+GNR-010") is dropped, or its pieces would read
	// as words and version numbers. So is the model repeated in the name,
	// and a bare number that only repeats the model's last part ("RX-78-4
	// ... Unit 4").
	s = reJoinedCode.ReplaceAllString(s, "$1")
	var lastPart string
	for _, m := range reModel.FindAllStringSubmatch(s, -1) {
		prefix := m[1][:strings.IndexAny(m[1], "-0123456789")]
		if _, stop := modelPrefixStop[prefix]; stop || len(prefix) < 2 {
			continue
		}
		k.Model = strings.ReplaceAll(m[1], "-", "")
		if i := strings.LastIndex(m[1], "-"); i >= 0 && strings.Trim(m[1][i+1:], "0123456789") == "" {
			lastPart = m[1][i+1:]
		}
		s = regexp.MustCompile(`\b`+regexp.QuoteMeta(m[0])+`(?:\b|$)`).ReplaceAllString(s, " ")
		break
	}

	distinct, tokens := map[string]struct{}{}, map[string]struct{}{}
	for _, tok := range reSplit.Split(s, -1) {
		tok = strings.Trim(tok, ".")
		if arabic, ok := reRoman[tok]; ok && tok != "v" {
			tok = arabic
		}
		if tok == "pb" {
			tok = "premium" // PlamoDX's prefix for Premium Bandai exclusives
		}
		if tok == "" || tok == lastPart {
			continue
		}
		if _, skip := noise[tok]; skip || reCatalogNo.MatchString(tok) || reHGPrefix.MatchString(tok) {
			continue
		}
		if isDistinct(tok) {
			distinct[tok] = struct{}{}
			continue
		}
		tokens[tok] = struct{}{}
	}
	k.Distinct, k.Tokens = sortedKeys(distinct), sortedKeys(tokens)
	return k
}

// seriesNumber finds the RG series number in a lowercased name. GeeksHeaven's
// "Model Kit 35" is a catalogue number for every grade, so it counts only
// when the name says RG.
func seriesNumber(s string) string {
	if m := reRGSerie.FindStringSubmatch(s); m != nil {
		return strings.TrimLeft(m[1], "0")
	}
	if reRGWord.MatchString(s) {
		if m := reKitNo.FindStringSubmatch(s); m != nil {
			return strings.TrimLeft(m[1], "0")
		}
	}
	return ""
}

// sameModel reports whether two model numbers (both non-empty) name the same
// suit. A "/FP" style suffix is ignored, and a code with a suffix that only
// adds letters to the other ("GN-0000" and "GN-0000GNHW/7SG") counts as the
// same. Without the suffix such letters tell kits apart (MS-06 and MS-06FZ,
// GP01 and GP01Fb).
func sameModel(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	longSuffixed := strings.Contains(b, "/")
	a, _, _ = strings.Cut(a, "/")
	b, _, _ = strings.Cut(b, "/")
	if a == b {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	rest, ok := strings.CutPrefix(b, a)
	return longSuffixed && ok && len(rest) >= 2 && strings.Trim(rest, "abcdefghijklmnopqrstuvwxyz") == "" && strings.ContainsAny(a, "0123456789")
}

func isDistinct(tok string) bool {
	if _, ok := distinctWords[tok]; ok {
		return true
	}
	if strings.HasPrefix(tok, "ver") {
		return true
	}
	// Bare numbers ("2.0", "00") are version/series markers.
	return strings.Trim(tok, "0123456789.") == ""
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NormalizeEAN returns the canonical 13-digit form of a barcode, or "" if
// s isn't plausibly one (wrong length, or a placeholder like 0000000000000).
func NormalizeEAN(s string) string {
	var digits strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch len(d) {
	case 12:
		d = "0" + d
	case 14:
		if d[0] != '0' {
			return ""
		}
		d = d[1:]
	case 13:
	default:
		return ""
	}
	if strings.Trim(d, string(d[0])) == "" {
		return "" // all one digit
	}
	return d
}
