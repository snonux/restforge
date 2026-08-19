package siren

import (
	"fmt"
	"slices"
	"strings"
)

// LinkByRel finds a link by rel among e.Links. Returns nil when there is
// none -- never an error, because a rel being absent is the server
// declining to offer something, not a malformed document. A link is
// matched by any of its rels, not just the first, because which one a
// server puts first is not part of the contract. A link with no href
// never matches, since it cannot be followed anyway.
func (e Entity) LinkByRel(rel string) *Link {
	for _, candidate := range e.Links {
		if candidate.Href == "" {
			continue
		}
		if slices.Contains(candidate.Rel, rel) {
			found := candidate
			return &found
		}
	}
	return nil
}

// Follow returns the href of the link with this rel, or "" when there is
// none.
func (e Entity) Follow(rel string) string {
	if link := e.LinkByRel(rel); link != nil {
		return link.Href
	}
	return ""
}

// ActionByName finds an offered action by name. Returns nil when there is
// none, which is a legitimate answer -- "not available right now" -- and
// not something to route around.
func (e Entity) ActionByName(name string) *Action {
	for _, candidate := range e.Actions {
		if candidate.Name == name {
			found := candidate
			return &found
		}
	}
	return nil
}

// Identifier returns the property key that names this entity, or "" when
// none of identifyingProperties has a usable value. Exposed so a caller
// can avoid printing the same value twice -- once as the label and again
// in the summary underneath it.
func (e Entity) Identifier() string {
	return identifierKey(e.Properties)
}

// Label is what to put on screen for this entity. See the precedence
// order documented on the shared label helper in decode.go.
func (e Entity) Label() string {
	return label(e.Title, "", e.Properties, e.Classes, e.Rel)
}

// VersionProblem returns a message when the server speaks a version this
// client was not written against, or "" to proceed. A missing or
// non-numeric apiVersion proceeds: a server that does not declare one is
// not making a claim this client can act on, and refusing to proceed
// would break it against every server that never had the field.
func (e Entity) VersionProblem() string {
	version, ok := e.Properties["apiVersion"].(float64)
	if !ok {
		return ""
	}
	if version > float64(supportedAPIVersion) {
		return fmt.Sprintf("server speaks apiVersion %v, this app understands %v",
			version, supportedAPIVersion)
	}
	return ""
}

// Summary is a one-line description of this document, for logging. It
// counts what is there rather than naming it, so it stays useful against a
// server this client has never seen. The class is the one exception -- it
// is shown, not interpreted, exactly as it is everywhere else in this
// package.
func (e Entity) Summary() string {
	return fmt.Sprintf("class [%s] %d link(s), %d action(s), %d entity(ies), %d propertie(s)",
		strings.Join(e.Classes, " "), len(e.Links), len(e.Actions), len(e.Entities), len(e.Properties))
}
