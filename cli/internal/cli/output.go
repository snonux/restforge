package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/snonux/restforge/cli/internal/render"
)

// This file holds the output-formatting helpers shared by the one-shot
// subcommands -- restforge get (this package), and the later restforge act
// and restforge quick, which reuse [PrintDocumentText] and [PrintJSON]
// rather than each re-implementing the row-to-line and JSON-passthrough
// rendering. See those tasks' descriptions for the coordination note that
// asks the first of the three to land to write the helper here.
//
// Text output is one row per line, tab-separated, so a caller can pipe it
// through cut/awk/grep and rely on a stable shape; --output json hands the
// raw decoded document straight through for jq.

// kindToken is the short, stable prefix each row line starts with, so a
// caller can grep for "^action\\t" or "^link\\t" without knowing a particular
// server's vocabulary. Kept lowercase and full-word for greppability.
func kindToken(k render.RowKind) string {
	switch k {
	case render.RowKindProperty:
		return "property"
	case render.RowKindEntity:
		return "entity"
	case render.RowKindLink:
		return "link"
	case render.RowKindAction:
		return "action"
	default:
		return "unknown"
	}
}

// targetHint is the short stand-in for a row's target, shown when the row
// has no Sublabel of its own -- a reference sub-entity or a link with no
// title, both of which carry only an href. It is presentation only: nothing
// here interprets a rel, class or name, so it stays free of any particular
// server's vocabulary (see ../../AGENTS.md, "Never build a URL").
func targetHint(t render.RowTarget) string {
	switch tt := t.(type) {
	case render.FetchTarget:
		return tt.Href
	case render.ActionTarget:
		return tt.Name
	case render.EmbeddedTarget:
		return "" // already in hand; there is no href to hint at
	case render.DetailTarget:
		return "" // a value, not a navigation target
	default:
		return "" // a future RowTarget renders as a blank hint, not a panic
	}
}

// RowLine renders one row as a single greppable line:
//
//	<kind>\t<label>\t<detail>
//
// where <detail> is the row's Sublabel, or a short target hint when the
// Sublabel is empty (a reference sub-entity, a link with no title). A line
// never contains a raw tab from a value, because render.Text JSON-encodes
// maps and slices and fmt.Sprints scalars, neither of which injects a tab.
func RowLine(row render.Row) string {
	detail := row.Sublabel
	if detail == "" {
		detail = targetHint(row.Target)
	}
	return fmt.Sprintf("%s\t%s\t%s", kindToken(row.Kind), row.Label, detail)
}

// PrintDocumentText writes doc to w as the document title on its own first
// line followed by one row per line (see [RowLine]). The title line lets a
// person orient; a caller that wants only the rows can skip the first line.
// nil doc writes nothing, so a caller with no document (a fetch that never
// landed) does not print a misleading empty success. The returned error is
// the first write failure (typically a closed stdout), so a caller can exit
// non-zero rather than report a silent partial success.
func PrintDocumentText(w io.Writer, doc *render.RenderedDocument) error {
	if doc == nil {
		return nil
	}
	if _, err := fmt.Fprintln(w, doc.Title); err != nil {
		return err
	}
	for _, row := range doc.Rows {
		if _, err := fmt.Fprintln(w, RowLine(row)); err != nil {
			return err
		}
	}
	return nil
}

// PrintJSON writes raw -- the already-decoded Siren document straight off
// the wire -- to w as pretty-printed JSON, for a caller piping into jq. raw
// is the decoded value, not a siren.Entity: re-marshalling the typed Entity
// would lose the server's own shape (Go structs here carry no JSON tags),
// so the one-shot commands pass the raw decoding through unchanged. The
// indented form is for a human reading the pipe; jq reformats it anyway.
func PrintJSON(w io.Writer, raw any) error {
	encoded, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode document as JSON: %w", err)
	}
	_, err = w.Write(append(encoded, '\n'))
	return err
}
