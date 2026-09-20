package reporter

import (
	"strings"
	"testing"

	"gunpla-collector/internal/store"
)

func TestFormatBaseline(t *testing.T) {
	msg := FormatBaseline("GeeksHeaven", 480)
	if !strings.Contains(msg, "480") || !strings.Contains(msg, "GeeksHeaven") {
		t.Errorf("baseline message missing expected content: %q", msg)
	}
}

func TestFormatDiff_NoChanges(t *testing.T) {
	msg := FormatDiff("GeeksHeaven", nil, nil, nil)
	if !strings.Contains(msg, "No changes today") {
		t.Errorf("expected no-changes message, got %q", msg)
	}
}

func TestFormatDiff_AllSections(t *testing.T) {
	newSets := []store.ReportItem{{Name: "New Kit", Grade: "MG", PriceCents: 5499, Currency: "EUR"}}
	removedSets := []store.ReportItem{{Name: "Gone Kit", Grade: "HG", PriceCents: 2000, Currency: "EUR"}}
	changes := []store.PriceChange{{Name: "Changed Kit", Grade: "RG", OldCents: 4000, NewCents: 4500, Currency: "EUR"}}

	msg := FormatDiff("GeeksHeaven", newSets, removedSets, changes)

	for _, want := range []string{"🆕", "❌", "💰", "New Kit", "€54.99", "Gone Kit", "€20.00", "Changed Kit", "€40.00", "€45.00"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestFormatDiff_SkipsEmptySections(t *testing.T) {
	newSets := []store.ReportItem{{Name: "New Kit", PriceCents: 1000}}
	msg := FormatDiff("GeeksHeaven", newSets, nil, nil)

	if strings.Contains(msg, "❌") {
		t.Errorf("expected Removed section to be skipped:\n%s", msg)
	}
	if strings.Contains(msg, "💰") {
		t.Errorf("expected Price changes section to be skipped:\n%s", msg)
	}
	if !strings.Contains(msg, "🆕") {
		t.Errorf("expected New section present:\n%s", msg)
	}
}

func TestFormatDiff_PriceChangeDirection(t *testing.T) {
	up := []store.PriceChange{{Name: "Up Kit", OldCents: 1000, NewCents: 1100}}
	msg := FormatDiff("Shop", nil, nil, up)
	if !strings.Contains(msg, "+10.0%") {
		t.Errorf("expected +10.0%% for price increase:\n%s", msg)
	}

	down := []store.PriceChange{{Name: "Down Kit", OldCents: 1000, NewCents: 900}}
	msg = FormatDiff("Shop", nil, nil, down)
	if !strings.Contains(msg, "-10.0%") {
		t.Errorf("expected -10.0%% for price decrease:\n%s", msg)
	}
}

func TestFormatDiff_UsesShopCurrencySymbol(t *testing.T) {
	newSets := []store.ReportItem{{Name: "USD Kit", PriceCents: 9200, Currency: "USD"}}
	msg := FormatDiff("Gundam Store", newSets, nil, nil)
	if !strings.Contains(msg, "$92.00") {
		t.Errorf("expected $92.00 for a USD-priced set, got:\n%s", msg)
	}
	if strings.Contains(msg, "€92.00") {
		t.Errorf("USD price must not be labeled with the Euro symbol:\n%s", msg)
	}
}

func TestFormatDiff_PriceChangeZeroOldPrice(t *testing.T) {
	changes := []store.PriceChange{{Name: "Freebie Kit", OldCents: 0, NewCents: 1000}}
	msg := FormatDiff("Shop", nil, nil, changes)
	if !strings.Contains(msg, "n/a") {
		t.Errorf("expected n/a percentage for zero old price, got:\n%s", msg)
	}
	if strings.Contains(msg, "Inf") || strings.Contains(msg, "NaN") {
		t.Errorf("percentage should not be Inf/NaN:\n%s", msg)
	}
}

func TestFormatDiff_EscapesHTMLInNames(t *testing.T) {
	newSets := []store.ReportItem{{Name: "RX-78 <Ver 2.0> R&D", PriceCents: 1000}}
	msg := FormatDiff("Shop", newSets, nil, nil)
	if strings.Contains(msg, "<Ver 2.0>") || strings.Contains(msg, "R&D") {
		t.Errorf("expected HTML special chars in the set name to be escaped:\n%s", msg)
	}
	if !strings.Contains(msg, "&lt;Ver 2.0&gt;") || !strings.Contains(msg, "R&amp;D") {
		t.Errorf("expected escaped HTML entities in name:\n%s", msg)
	}
}

// FormatDiff's own <b> tags must survive unescaped — only untrusted
// content (set names) goes through esc().
func TestFormatDiff_DoesNotEscapeItsOwnHTMLTags(t *testing.T) {
	msg := FormatDiff("Shop", []store.ReportItem{{Name: "Kit", PriceCents: 1000}}, nil, nil)
	if !strings.Contains(msg, "<b>Shop</b>") {
		t.Errorf("expected literal <b>Shop</b>, got:\n%s", msg)
	}
	if !strings.Contains(msg, "<b>New</b>") {
		t.Errorf("expected literal <b>New</b> section header, got:\n%s", msg)
	}
}
