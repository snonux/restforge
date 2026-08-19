package siren

// supportedAPIVersion is the highest apiVersion this client was written
// against. A server carrying a higher one may have changed something this
// client would silently misread, so Entity.VersionProblem stops and says
// so instead of guessing. This is a plain property check -- nothing about
// it is specific to any API. Matches supportedApiVersion in
// flutter/lib/models/siren.dart and SUPPORTED_API_VERSION in
// pebble/src/pkjs/siren.js.
const supportedAPIVersion = 1

// identifyingProperties are properties that identify *which* thing an
// entity is, tried in order when the document carries no wording of its
// own (no title).
//
// This is a convention of the Siren format rather than knowledge of any
// particular server: Siren itself uses "name" to identify actions and
// fields, and "name"/"id" are what hypermedia APIs conventionally call the
// identifier of an entity.
//
// It earns its place empirically. A collection whose members share one
// class renders as a list of identical rows without it, and a list where
// every row reads the same is not a list. What a caller still must not do
// is interpret the value: it is displayed exactly as sent, and no meaning
// is attached to it beyond "this is what it is called".
var identifyingProperties = []string{"name", "title", "id"}

// Field is one field an action's form offers to fill in.
//
// Kept deliberately thin: this package never inspects a field's Type or
// Value beyond counting fields -- how a field is rendered and validated is
// a concern of the render/action pipeline, not of this document model.
type Field struct {
	Name  string
	Type  string
	Value any
	Title string

	// Required is whether the server marked this field required. Siren has
	// no own concept of it; this is the same server extension property
	// pebble/src/pkjs/actions.js reads off field.required -- a plain
	// boolean member on the field object, not a Siren-defined one. Absent
	// or anything other than JSON true means "not required", the same
	// tolerant-default rule every other optional member here follows.
	//
	// Nothing in this package *acts* on it -- that would be policy, not a
	// document lookup, and belongs to a later action package, exactly as
	// siren.js never inspects field.value either.
	Required bool
}

// Label is what to show for this field on screen. See the label
// precedence note on Entity.Label.
func (f Field) Label() string {
	return label(f.Title, f.Name, nil, nil, nil)
}

// Link is a hypermedia link: a rel, an href to follow it to, and nothing
// this client interprets beyond that.
type Link struct {
	Rel []string

	// Href is "" when the server sent none. Such a link cannot be
	// followed, so Entity.LinkByRel and Entity.Follow never match it --
	// mirrors link() in siren.js, which only matches list entries with a
	// string href.
	Href    string
	Classes []string
	Title   string
	Type    string
}

// Label is what to show for this link on screen. See the label precedence
// note on Entity.Label.
func (l Link) Label() string {
	return label(l.Title, "", nil, l.Classes, l.Rel)
}

// Action is an action a server is currently offering: a method and href to
// send a request to, and the fields its form asks for.
type Action struct {
	Name string

	// Href is "" when the server sent none. An action with no href cannot
	// be acted on; a caller checking for one before offering the action
	// stays as generic as everything else here.
	Href string

	// Method is always uppercase, and defaults to Siren's own default,
	// GET, when the server did not say -- mirrors method() in siren.js. A
	// server that means to change something says so explicitly.
	Method  string
	Classes []string
	Title   string
	Type    string
	Fields  []Field
}

// Label is what to show for this action on screen. See the label
// precedence note on Entity.Label.
func (a Action) Label() string {
	return label(a.Title, a.Name, nil, a.Classes, nil)
}

// Entity is a Siren entity: a document in its own right when it is the
// root, or a sub-entity when it is nested inside another entity's
// Entities.
//
// Siren allows a sub-entity to be either an embedded representation (its
// own properties, sub-entities, links and actions) or a bare reference to
// one (just a class, a rel to its parent, and an href) -- see IsReference.
// Every slice and map here reads as empty when the server sent nothing,
// whether that member was absent or explicitly wrong-typed: Go's nil map
// and nil slice are safe to read (index, range, len) exactly like an empty
// one, so unlike Dart's null-safe String?/List?, no caller here ever needs
// a null check before reading one.
type Entity struct {
	Classes    []string
	Properties map[string]any

	// Entities are sub-entities, each of which may itself be IsReference.
	Entities []Entity
	Links    []Link
	Actions  []Action
	Title    string

	// Rel is this entity's relation to its parent. Only meaningful when
	// this Entity is a sub-entity; empty for a root document.
	Rel []string

	// Href is present ("" is absent) when this entity carries an href --
	// always non-empty for a reference sub-entity, optionally present on
	// an embedded one too.
	Href string

	// IsReference is true when this sub-entity is only a reference -- a
	// bare href -- rather than an embedded representation. Mirrors
	// isReference() in siren.js: an entity counts as embedded, not a
	// reference, the moment it carries a properties object or an entities
	// array of its own, even an empty one -- the server chose to say
	// "nothing here" rather than "look elsewhere", and that distinction is
	// not this client's to erase.
	IsReference bool
}
