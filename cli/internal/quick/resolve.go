package quick

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

// BackendFor resolves item against the configured backends, by base URL --
// see the package comment -- so that renaming or reordering the backend
// list never re-points a saved shortcut at a different server. Returns
// nil, nil when the backend it referred to is gone: a real, reportable
// answer, not a bug, for a caller to show (e.g. "backend removed") rather
// than silently drop the shortcut. Mirrors quick_service.dart's
// backendFor()/quick.js's backendFor().
//
// Single-shot: loads the backend list itself via config.LoadBackends. A
// caller that already has the list (resolving several shortcuts at once)
// should use BackendsFor instead, which resolves against a supplied list
// and does no storage read -- N BackendFor calls cost N config reads,
// which is the wrong shape for a list of shortcuts.
func BackendFor(item QuickItem) (*backend.Backend, error) {
	backends, err := config.LoadBackends()
	if err != nil {
		return nil, fmt.Errorf("quick: could not load backends: %w", err)
	}
	return resolveBackend(item, backends), nil
}

// BackendsFor resolves items against backends in memory, by base URL -- the
// same matching BackendFor uses, but without a storage read: pass a backend
// list the caller has already loaded once so resolving N shortcuts does not
// cost N config reads on a hot path. Returns nil per item whose backend is
// gone, exactly like BackendFor. Pure function of its arguments; the
// by-base-URL matching lives here so a caller does not re-implement it.
// Mirrors quick_service.dart's backendsFor().
func BackendsFor(items []QuickItem, backends []backend.Backend) []*backend.Backend {
	out := make([]*backend.Backend, len(items))
	for i, item := range items {
		out[i] = resolveBackend(item, backends)
	}
	return out
}

// resolveBackend returns a pointer to the first entry in backends whose
// BaseURL matches item's -- the by-base-URL matching both BackendFor and
// BackendsFor share, so the rule has one home. Returns nil when none
// matches.
func resolveBackend(item QuickItem, backends []backend.Backend) *backend.Backend {
	for i := range backends {
		if backends[i].BaseURL == item.BaseURL {
			return &backends[i]
		}
	}
	return nil
}
