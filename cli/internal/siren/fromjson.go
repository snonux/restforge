package siren

// ---------------------------------------------------------------------------
// Building the typed model out of already-JSON-decoded values (map[string]any,
// []any, and the JSON scalar types). Each *FromJSON function accepts the
// dynamic value straight out of encoding/json's default decoding -- never
// only a map -- because a malformed document might hand a sub-entity, a
// link or an action anything at all (a string, a number, null); mirrors the
// *.fromJson factories in flutter/lib/models/siren.dart, which take
// `dynamic json` for the same reason.
// ---------------------------------------------------------------------------

// FieldFromJSON builds a Field from a decoded JSON value, tolerating a
// missing or wrongly-typed member by degrading to Field's zero value for
// that member. Never panics.
func FieldFromJSON(v any) Field {
	m := asMap(v)
	return Field{
		Name:     asNonEmptyString(m["name"]),
		Type:     asNonEmptyString(m["type"]),
		Value:    m["value"],
		Title:    asNonEmptyString(m["title"]),
		Required: m["required"] == true,
	}
}

// LinkFromJSON builds a Link from a decoded JSON value. Never panics.
func LinkFromJSON(v any) Link {
	m := asMap(v)
	return Link{
		Rel:     asStringList(m["rel"]),
		Href:    asNonEmptyString(m["href"]),
		Classes: asStringList(m["class"]),
		Title:   asNonEmptyString(m["title"]),
		Type:    asNonEmptyString(m["type"]),
	}
}

// ActionFromJSON builds an Action from a decoded JSON value. Never panics.
func ActionFromJSON(v any) Action {
	m := asMap(v)
	return Action{
		Name:    asNonEmptyString(m["name"]),
		Href:    asNonEmptyString(m["href"]),
		Method:  asMethod(m["method"]),
		Classes: asStringList(m["class"]),
		Title:   asNonEmptyString(m["title"]),
		Type:    asNonEmptyString(m["type"]),
		Fields:  fieldsFromJSON(m["fields"]),
	}
}

func fieldsFromJSON(v any) []Field {
	items := asList(v)
	fields := make([]Field, 0, len(items))
	for _, item := range items {
		fields = append(fields, FieldFromJSON(item))
	}
	return fields
}

// EntityFromJSON builds an Entity from a decoded JSON value -- the root
// document, or a sub-entity out of another entity's "entities" array.
// Never panics; a value that is not a JSON object at all decodes to
// Entity's zero value, the same "nothing offered" answer as an object
// missing every member.
func EntityFromJSON(v any) Entity {
	m := asMap(v)
	rawProperties := m["properties"]
	rawEntities := m["entities"]
	href := asNonEmptyString(m["href"])
	return Entity{
		Classes:     asStringList(m["class"]),
		Properties:  asMap(rawProperties),
		Entities:    entitiesFromJSON(rawEntities),
		Links:       linksFromJSON(m["links"]),
		Actions:     actionsFromJSON(m["actions"]),
		Title:       asNonEmptyString(m["title"]),
		Rel:         asStringList(m["rel"]),
		Href:        href,
		IsReference: isReference(href, rawProperties, rawEntities),
	}
}

// isReference reports whether an entity carrying href should be treated as
// a bare reference rather than an embedded representation: it must not
// also carry a properties object or an entities array of its own, even an
// empty one -- the server chose to say "nothing here" rather than "look
// elsewhere", and that distinction is not this client's to erase. Mirrors
// isReference() in siren.js.
func isReference(href string, rawProperties, rawEntities any) bool {
	_, propertiesIsObject := rawProperties.(map[string]any)
	_, entitiesIsArray := rawEntities.([]any)
	return href != "" && !propertiesIsObject && !entitiesIsArray
}

func entitiesFromJSON(v any) []Entity {
	items := asList(v)
	entities := make([]Entity, 0, len(items))
	for _, item := range items {
		entities = append(entities, EntityFromJSON(item))
	}
	return entities
}

func linksFromJSON(v any) []Link {
	items := asList(v)
	links := make([]Link, 0, len(items))
	for _, item := range items {
		links = append(links, LinkFromJSON(item))
	}
	return links
}

func actionsFromJSON(v any) []Action {
	items := asList(v)
	actions := make([]Action, 0, len(items))
	for _, item := range items {
		actions = append(actions, ActionFromJSON(item))
	}
	return actions
}
