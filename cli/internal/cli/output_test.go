package cli

import (
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/render"
)

// TestRowLine pins the shared row-to-line format that restforge get prints
// and restforge act / restforge quick will reuse. It is table-driven so each
// row kind and the sublabel-or-target-hint fallback has its own case.
func TestRowLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  render.Row
		want string
	}{
		{
			name: "property uses its value as detail",
			row:  render.Row{Label: "kettle", Sublabel: "cold", Kind: render.RowKindProperty},
			want: "property\tkettle\tcold",
		},
		{
			name: "entity uses its summary as detail",
			row:  render.Row{Label: "Top shelf", Sublabel: "jars 4  name top", Kind: render.RowKindEntity},
			want: "entity\tTop shelf\tjars 4  name top",
		},
		{
			name: "reference entity with no summary falls back to its href",
			row: render.Row{Label: "a ref", Sublabel: "", Kind: render.RowKindEntity,
				Target: render.FetchTarget{Href: "/ref"}},
			want: "entity\ta ref\t/ref",
		},
		{
			name: "link with a title shows its rels as detail",
			row: render.Row{Label: "Shelves", Sublabel: "shelves", Kind: render.RowKindLink,
				Target: render.FetchTarget{Href: "/shelves"}},
			want: "link\tShelves\tshelves",
		},
		{
			name: "link with no title falls back to its href",
			row: render.Row{Label: "self", Sublabel: "", Kind: render.RowKindLink,
				Target: render.FetchTarget{Href: "/"}},
			want: "link\tself\t/",
		},
		{
			name: "action shows method and field count as detail",
			row:  render.Row{Label: "Brew tea", Sublabel: "POST, 1 field(s)", Kind: render.RowKindAction},
			want: "action\tBrew tea\tPOST, 1 field(s)",
		},
		{
			name: "action with no method falls back to its name",
			row: render.Row{Label: "peek", Sublabel: "", Kind: render.RowKindAction,
				Target: render.ActionTarget{Name: "peek"}},
			want: "action\tpeek\tpeek",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RowLine(tc.row)
			if got != tc.want {
				t.Errorf("RowLine = %q, want %q", got, tc.want)
			}
			if strings.Count(got, "\t") != 2 {
				t.Errorf("RowLine = %q, want exactly two tabs", got)
			}
		})
	}
}

// TestPrintDocumentTextNilPrintsNothing guards the "never print a misleading
// empty success" rule: a fetch that never landed (nil document) writes
// nothing rather than a blank title line a caller might mistake for a doc.
func TestPrintDocumentTextNilPrintsNothing(t *testing.T) {
	var b strings.Builder
	if err := PrintDocumentText(&b, nil); err != nil {
		t.Errorf("PrintDocumentText(nil) error = %v, want nil", err)
	}
	if b.Len() != 0 {
		t.Errorf("PrintDocumentText(nil) wrote %q, want nothing", b.String())
	}
}
