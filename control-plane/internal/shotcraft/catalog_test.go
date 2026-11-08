package shotcraft

import (
	"strings"
	"testing"
)

func TestPinnedLibraryIncludesEveryRecipeAndExecutableVariant(t *testing.T) {
	cards := List("")
	if len(cards) != 124 {
		t.Fatalf("pinned library has %d cards", len(cards))
	}
	ids := map[string]bool{}
	for _, card := range cards {
		if recipe, err := Read(card.Recipe); err != nil || recipe == "" {
			t.Fatalf("missing recipe for %s: %v", card.ID, err)
		}
		for _, style := range card.Styles {
			key := card.ID + "/" + style.ID
			if ids[key] {
				t.Fatalf("duplicate variant: %s", key)
			}
			ids[key] = true
			if source, err := Read(style.Source); err != nil || source == "" {
				t.Fatalf("missing original source for %s: %v", key, err)
			}
			tpl := Compiled(card, style)
			if tpl.DurationFrames < 1 || !strings.Contains(tpl.Source, "export default") || !strings.Contains(tpl.Source, "Vincentwei1021") {
				t.Fatalf("missing attributed template for %s: %s", key, tpl.NeedsAdapter)
			}
		}
	}
	if len(ids) != 150 {
		t.Fatalf("pinned library has %d variants", len(ids))
	}
	if _, _, ok := Lookup("chart-live-moves/axis-rescale-shock"); !ok {
		t.Fatal("explicit variant does not resolve")
	}
	if _, _, ok := Lookup("chart-live-moves/absent"); ok {
		t.Fatal("unknown variant resolved")
	}
}
