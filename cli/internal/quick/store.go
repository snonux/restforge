package quick

import (
	"fmt"
	"log"

	"github.com/snonux/restforge/cli/internal/config"
)

// Warnf is called with a formatted message whenever Add or Save degrades
// around a problem instead of failing outright -- an incomplete shortcut
// refused, the cap reached. It defaults to the standard logger, mirroring
// config.Warnf's role as this package's own test hook: production code
// should not need to replace it.
var Warnf = func(format string, args ...any) {
	log.Printf("quick: "+format, args...)
}

// Load returns the stored shortcuts, normalised, usable and capped at
// MaxQuick. config.LoadQuick already never throws around a missing file,
// corrupt TOML or insecure permissions (reporting those through
// config.Warnf); the filtering added here is the domain half --
// Normalise, usable, MaxQuick -- that config.LoadQuick deliberately leaves
// to this package (see its doc comment). The one error this can return is
// config.LoadQuick's own: the config path could not be resolved. Mirrors
// quick_service.dart's load()/quick.js's load().
func Load() ([]QuickItem, error) {
	records, err := config.LoadQuick()
	if err != nil {
		return nil, fmt.Errorf("quick: could not load: %w", err)
	}
	return filterUsable(fromRecords(records)), nil
}

// Save persists list: normalises, drops anything unusable and caps at
// MaxQuick exactly like Load reads it back, then writes through
// config.SaveQuick. Returns the persisted list on success. A write
// failure (a full disk, a missing parent that could not be created) comes
// back as an error rather than a partial, silently-inconsistent write.
// Mirrors quick_service.dart's save()/quick.js's save().
func Save(list []QuickItem) ([]QuickItem, error) {
	clean := filterUsable(list)
	if err := config.SaveQuick(toRecords(clean)); err != nil {
		return nil, fmt.Errorf("quick: could not save: %w", err)
	}
	return clean, nil
}

// filterUsable normalises every entry and keeps only the ones that pass
// usable, capped at MaxQuick -- the shared step Load and Save both apply,
// mirroring config's own filterBackends (and, one level below that,
// quick_service.dart's _normaliseStoredList/_capAndFilter, which the
// package comment explains are not extracted into a shared helper with
// config's).
func filterUsable(items []QuickItem) []QuickItem {
	out := make([]QuickItem, 0, len(items))
	for _, item := range items {
		if len(out) >= MaxQuick {
			break
		}
		n := Normalise(item)
		if usable(n) {
			out = append(out, n)
		}
	}
	return out
}

// fromRecords converts config-owned QuickRecords (read from disk) into
// this package's QuickItem, the mirror image of toRecords. See
// config.QuickRecord's comment for why the conversion lives here rather
// than in internal/config.
func fromRecords(records []config.QuickRecord) []QuickItem {
	out := make([]QuickItem, len(records))
	for i, r := range records {
		out[i] = QuickItem{
			Label:       r.Label,
			BackendName: r.BackendName,
			BaseURL:     r.BaseURL,
			Kind:        kindFromString(r.Kind),
			Holder:      r.Holder,
			Name:        r.Name,
			Href:        r.Href,
		}
	}
	return out
}

// toRecords converts QuickItems into the config-owned QuickRecord shape
// for config.SaveQuick, the mirror image of fromRecords.
func toRecords(items []QuickItem) []config.QuickRecord {
	out := make([]config.QuickRecord, len(items))
	for i, item := range items {
		out[i] = config.QuickRecord{
			Label:       item.Label,
			BackendName: item.BackendName,
			BaseURL:     item.BaseURL,
			Kind:        item.Kind.String(),
			Holder:      item.Holder,
			Name:        item.Name,
			Href:        item.Href,
		}
	}
	return out
}

// kindFromString is the inverse of QuickKind.String: only the literal
// "action" reads back as KindAction, everything else (including
// "document", empty, or hand-edited garbage) reads as KindDocument --
// matching quick.js's normalise's `item.kind === KIND_ACTION ? ... `
// ternary default.
func kindFromString(s string) QuickKind {
	if s == "action" {
		return KindAction
	}
	return KindDocument
}
