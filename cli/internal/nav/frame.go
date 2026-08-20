package nav

import (
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/siren"
)

// frame is one entry on the navigation stack: a document, the backend it
// was fetched from, the href it can be re-fetched from, and the title it is
// shown under. Mirrors the { entity, href, title } stack frame shape in
// nav.js and _StackFrame in nav_service.dart, plus be (this Go port's own
// addition, p31): every push site (pushLocked, pushIfCurrent,
// fetchAndApply) records the backend.Backend its own GET actually pinned
// against, so a later Refresh or a rendered FetchTarget can pin its own
// fetch to the backend this document truly belongs to instead of re-reading
// whatever Nav.current happens to be at that later moment -- see
// Nav.Refresh and render.Document for the two callers this closes the gap
// for.
type frame struct {
	entity siren.Entity

	be backend.Backend

	// href is "" for a sub-entity that arrived embedded rather than
	// linked (see Nav.OpenEmbedded) -- Siren allows either, and an
	// embedded entity simply has no address of its own to re-fetch from.
	href string

	title string
}
