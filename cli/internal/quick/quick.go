package quick

import "strings"

// MaxQuick caps how many shortcuts may be saved. More than this and the
// opening screen stops being quicker than browsing. Matches
// QuickService.maxQuick (quick_service.dart) and MAX_QUICK (quick.js).
const MaxQuick = 12

// MaxFieldLength caps every free-text field, for the same reason
// backend.MaxFieldLength does -- generous, because the limit that actually
// matters is screen space. Matches QuickService.maxFieldLength.
const MaxFieldLength = 256

// QuickKind is what a shortcut points at. Mirrors KIND_ACTION/KIND_DOCUMENT
// in quick.js, as a closed Go type rather than a pair of string constants.
type QuickKind int

const (
	// KindAction is a shortcut to an action, looked up by Holder and Name
	// at run time -- see the package comment for why it never stores the
	// action's own href.
	KindAction QuickKind = iota
	// KindDocument is a shortcut to a document, fetched by Href.
	KindDocument
)

// String names a QuickKind the way it is stored on disk ("action" or
// "document"). Any value other than KindAction reads as "document",
// matching the ternary quick.js's normalise and this package's own
// Normalise use to coerce a drifted or hand-edited Kind.
func (k QuickKind) String() string {
	if k == KindAction {
		return "action"
	}
	return "document"
}

// QuickItem is one saved shortcut. A plain value with no behaviour beyond
// reading and persisting itself, the Go counterpart to quick_service.dart's
// QuickItem class.
//
// Holder and Name are what an action shortcut is looked up by; Href is
// what a document shortcut is fetched from -- see the package comment for
// why an action never stores an href of its own. BackendName is the
// backend's display name at the moment the shortcut was saved: kept for
// schema parity with quick.js (which stores it the same way) even though
// nothing in this package reads it back -- BackendFor/BackendsFor resolve
// the current name by BaseURL instead, precisely so a rename is reflected
// rather than frozen at save time.
type QuickItem struct {
	Label       string
	BackendName string
	BaseURL     string
	Kind        QuickKind
	Holder      string
	Name        string
	Href        string
}

// Normalise coerces item into the shape Load/Save/Add expect: every
// free-text field trimmed of surrounding whitespace and capped at
// MaxFieldLength, and Kind coerced to a known value (see QuickKind.String).
// It does not reject anything -- usable does that. Input that has drifted
// (an older layout, a hand-edited config file) degrades to something
// usable instead of taking a caller down. Mirrors quick_service.dart's
// normalise/quick.js's normalise.
func Normalise(item QuickItem) QuickItem {
	kind := KindDocument
	if item.Kind == KindAction {
		kind = KindAction
	}
	return QuickItem{
		Label:       trim(item.Label),
		BackendName: trim(item.BackendName),
		BaseURL:     trim(item.BaseURL),
		Kind:        kind,
		Holder:      trim(item.Holder),
		Name:        trim(item.Name),
		Href:        trim(item.Href),
	}
}

// usable rejects a shortcut that could not be acted on. An action needs
// somewhere to look itself up next time; a document needs an address.
// Mirrors usable() in quick_service.dart/quick.js.
func usable(item QuickItem) bool {
	if item.Label == "" || item.BaseURL == "" {
		return false
	}
	if item.Kind == KindAction {
		return item.Holder != "" && item.Name != ""
	}
	return item.Href != ""
}

// same reports whether a and b point at the same target -- used by Add to
// decide whether saving is an update rather than a new entry. Mirrors
// same() in quick_service.dart/quick.js.
func same(a, b QuickItem) bool {
	return a.BaseURL == b.BaseURL &&
		a.Kind == b.Kind &&
		a.Holder == b.Holder &&
		a.Name == b.Name &&
		a.Href == b.Href
}

// trim mirrors backend.clean/QuickService._trim: surrounding whitespace
// stripped, capped at MaxFieldLength runes (not bytes, so truncation can
// never split a multi-byte UTF-8 character and leave invalid UTF-8
// behind).
func trim(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > MaxFieldLength {
		r = r[:MaxFieldLength]
	}
	return string(r)
}
