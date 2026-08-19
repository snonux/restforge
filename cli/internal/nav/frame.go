package nav

import "github.com/snonux/restforge/cli/internal/siren"

// frame is one entry on the navigation stack: a document, the href it can
// be re-fetched from, and the title it is shown under. Mirrors the
// { entity, href, title } stack frame shape in nav.js and _StackFrame in
// nav_service.dart.
type frame struct {
	entity siren.Entity

	// href is "" for a sub-entity that arrived embedded rather than
	// linked (see Nav.OpenEmbedded) -- Siren allows either, and an
	// embedded entity simply has no address of its own to re-fetch from.
	href string

	title string
}
