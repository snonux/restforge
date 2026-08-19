// Port of flutter/test/services/render_service_test.dart as Go
// table-driven tests. See AGENTS.md section 5 ("Test style") and this
// package's doc comment for why: rendering must not interpret, so most of
// what follows asserts that values survive untouched and that nothing is
// dropped.
//
// One deliberate difference from the Dart source: siren.Entity.Properties
// is a plain, unordered Go map (see sortedPropertyKeys in rows.go), so this
// package renders properties in sorted-key order rather than the server's
// own order. Everywhere the Dart fixture's expected order would depend on
// property insertion order, the fixtures and expectations below are
// adjusted to sorted-key order instead; the behaviour under test --
// nothing dropped, nothing interpreted, the label skipped from its own
// summary -- is unchanged.
//
// Vocabulary in the fixtures below is deliberately made up (never a real
// API's), the same way DOC is in test-render.js and the Dart fixture.
package render_test

import (
	"fmt"
	"testing"

	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/siren"
)

// docJSON mirrors the `doc` fixture in render_service_test.dart.
var docJSON = map[string]any{
	"class": []any{"pantry"},
	"title": "The pantry",
	"properties": map[string]any{
		"kettle":  "cold",
		"brews":   float64(0),
		"tidy":    false,
		"missing": nil,
	},
	"entities": []any{
		map[string]any{
			"class": []any{"shelf"},
			"title": "Top shelf",
			"properties": map[string]any{
				"name":      "top",
				"jars":      float64(4),
				"reachable": true,
			},
		},
		map[string]any{
			"class": []any{"shelf"},
			"title": "Bottom shelf",
			"href":  "/shelves/bottom",
		},
	},
	"links": []any{
		map[string]any{"rel": []any{"self"}, "href": "/"},
		map[string]any{"rel": []any{"shelves"}, "href": "/shelves"},
	},
	"actions": []any{
		map[string]any{
			"name":   "brew",
			"title":  "Brew a pot of tea",
			"method": "POST",
			"href":   "/brew",
			"fields": []any{
				map[string]any{"name": "strength", "type": "range"},
			},
		},
		map[string]any{"name": "peek", "href": "/peek"},
	},
}

func doc(t *testing.T) siren.Entity {
	t.Helper()
	return siren.EntityFromJSON(docJSON)
}

func kindsOf(page render.RenderedDocument) []render.RowKind {
	kinds := make([]render.RowKind, len(page.Rows))
	for i, row := range page.Rows {
		kinds[i] = row.Kind
	}
	return kinds
}

// rowNamed finds the row with the given label, or fails the test loudly --
// mirrors rowNamed() in render_service_test.dart, which throws a
// StateError rather than returning a nullable Row so a missing row fails
// the assertion it is part of, not a later nil check.
func rowNamed(t *testing.T, page render.RenderedDocument, label string) render.Row {
	t.Helper()
	for _, row := range page.Rows {
		if row.Label == label {
			return row
		}
	}
	t.Fatalf("no row %q", label)
	panic("unreachable")
}

// targetKind returns a short debug label for a RowTarget's concrete type.
// This is the one switch over render.RowTarget this package's tests
// contain, and it carries the default-panics-as-canary case that
// target.go's doc comment asks every such switch to have: Go cannot check
// this switch for exhaustiveness the way Dart checks a switch over a
// sealed class, so a fifth RowTarget implementation with no case here
// fails loudly the first time a test exercises it, instead of silently
// mis-describing it in a failure message.
func targetKind(target render.RowTarget) string {
	switch target.(type) {
	case render.DetailTarget:
		return "detail"
	case render.FetchTarget:
		return "fetch"
	case render.EmbeddedTarget:
		return "embedded"
	case render.ActionTarget:
		return "action"
	default:
		panic(fmt.Sprintf("render_test: unreachable RowTarget type %T", target))
	}
}

func TestOrderAndCompleteness(t *testing.T) {
	page := render.Document(ref(doc(t)), "")

	t.Run("the title comes from the document", func(t *testing.T) {
		if page.Title != "The pantry" {
			t.Errorf("Title = %q, want %q", page.Title, "The pantry")
		}
	})

	t.Run("rows are in document order", func(t *testing.T) {
		// Siren's own order: properties, entities, links, actions.
		// Choosing a different one would mean deciding which part of
		// somebody else's document matters most.
		want := []render.RowKind{
			render.RowKindProperty, render.RowKindProperty, render.RowKindProperty, render.RowKindProperty,
			render.RowKindEntity, render.RowKindEntity,
			render.RowKindLink, render.RowKindLink,
			render.RowKindAction, render.RowKindAction,
		}
		got := kindsOf(page)
		if len(got) != len(want) {
			t.Fatalf("len(kinds) = %d, want %d (%v)", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("kinds[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("nothing is dropped", func(t *testing.T) {
		d := doc(t)
		want := 4 + len(d.Entities) + len(d.Links) + len(d.Actions)
		if len(page.Rows) != want {
			t.Errorf("len(Rows) = %d, want %d", len(page.Rows), want)
		}
	})

	t.Run("the self link is offered like any other", func(t *testing.T) {
		// Including the ones a client might be tempted to hide.
		rowNamed(t, page, "self")
	})
}

func TestValuesAreNotInterpreted(t *testing.T) {
	page := render.Document(ref(doc(t)), "")

	cases := []struct {
		label string
		want  string
	}{
		{"tidy", "false"},   // a false boolean stays false
		{"brews", "0"},      // a zero stays zero
		{"missing", "null"}, // null is a value the server chose to send
		{"kettle", "cold"},  // a string is passed through
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if got := rowNamed(t, page, c.label).Sublabel; got != c.want {
				t.Errorf("Sublabel = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTargets(t *testing.T) {
	page := render.Document(ref(doc(t)), "")

	t.Run("a property opens the reading window", func(t *testing.T) {
		target := rowNamed(t, page, "kettle").Target
		detail, ok := target.(render.DetailTarget)
		if !ok {
			t.Fatalf("target kind = %s, want detail", targetKind(target))
		}
		if detail.Body != "cold" {
			t.Errorf("Body = %q, want cold", detail.Body)
		}
	})

	t.Run("an embedded entity is opened locally", func(t *testing.T) {
		// Already in hand: opening it asks the server nothing, and asking
		// again could legitimately return something different.
		target := rowNamed(t, page, "Top shelf").Target
		embedded, ok := target.(render.EmbeddedTarget)
		if !ok {
			t.Fatalf("target kind = %s, want embedded", targetKind(target))
		}
		if embedded.Index != 0 {
			t.Errorf("Index = %d, want 0", embedded.Index)
		}
	})

	t.Run("an embedded entity is summarised", func(t *testing.T) {
		// Sorted-key order (jars, name, reachable), not document order:
		// see the package-level comment on why.
		if got := rowNamed(t, page, "Top shelf").Sublabel; got != "jars 4  name top" {
			t.Errorf("Sublabel = %q, want %q", got, "jars 4  name top")
		}
	})

	t.Run("a referenced entity is fetched", func(t *testing.T) {
		target := rowNamed(t, page, "Bottom shelf").Target
		fetch, ok := target.(render.FetchTarget)
		if !ok {
			t.Fatalf("target kind = %s, want fetch", targetKind(target))
		}
		if fetch.Href != "/shelves/bottom" {
			t.Errorf("Href = %q, want /shelves/bottom", fetch.Href)
		}
	})

	t.Run("a link is fetched by its href", func(t *testing.T) {
		target := rowNamed(t, page, "shelves").Target
		fetch, ok := target.(render.FetchTarget)
		if !ok {
			t.Fatalf("target kind = %s, want fetch", targetKind(target))
		}
		if fetch.Href != "/shelves" {
			t.Errorf("Href = %q, want /shelves", fetch.Href)
		}
	})
}

func TestActions(t *testing.T) {
	page := render.Document(ref(doc(t)), "")

	t.Run("an action shows its title and is addressed by name", func(t *testing.T) {
		// The title is the server's sentence for a person; the name is an
		// identifier. The sentence wins.
		row := rowNamed(t, page, "Brew a pot of tea")
		action, ok := row.Target.(render.ActionTarget)
		if !ok {
			t.Fatalf("target kind = %s, want action", targetKind(row.Target))
		}
		if action.Name != "brew" {
			t.Errorf("Name = %q, want brew", action.Name)
		}
	})

	t.Run("an action shows its method and field count", func(t *testing.T) {
		if got := rowNamed(t, page, "Brew a pot of tea").Sublabel; got != "POST, 1 field(s)" {
			t.Errorf("Sublabel = %q, want %q", got, "POST, 1 field(s)")
		}
	})

	t.Run("an action without a title falls back to its name", func(t *testing.T) {
		rowNamed(t, page, "peek")
	})

	t.Run("an action without a method defaults to GET", func(t *testing.T) {
		// Siren's default method, for an action that does not say.
		if got := rowNamed(t, page, "peek").Sublabel; got != "GET" {
			t.Errorf("Sublabel = %q, want GET", got)
		}
	})
}

func TestEntitiesSharingAClass(t *testing.T) {
	// Regression: entities distinguished only by a property.
	hosts := siren.EntityFromJSON(map[string]any{
		"class": []any{"status"},
		"entities": []any{
			map[string]any{
				"class":      []any{"host", "f"},
				"properties": map[string]any{"ip": "10.0.0.1", "ms": float64(15), "name": "f0"},
			},
			map[string]any{
				"class":      []any{"host", "f"},
				"properties": map[string]any{"ip": "10.0.0.2", "ms": float64(24), "name": "f1"},
			},
		},
	})
	page := render.Document(&hosts, "")

	t.Run("entities sharing a class are told apart", func(t *testing.T) {
		if page.Rows[0].Label != "f0" {
			t.Errorf("Rows[0].Label = %q, want f0", page.Rows[0].Label)
		}
		if page.Rows[1].Label != "f1" {
			t.Errorf("Rows[1].Label = %q, want f1", page.Rows[1].Label)
		}
	})

	t.Run("the summary does not repeat the label", func(t *testing.T) {
		// The label is not repeated below itself: on this screen the
		// summary has room for two facts and one of them must not be the
		// heading again.
		if got := page.Rows[0].Sublabel; got != "ip 10.0.0.1  ms 15" {
			t.Errorf("Sublabel = %q, want %q", got, "ip 10.0.0.1  ms 15")
		}
	})
}

// TestDescribe has no Dart counterpart -- render_service_test.dart never
// exercises describe() -- but the task this package was built for asks for
// Describe explicitly, so it gets its own coverage here rather than going
// untested. Mirrors the "key: value" pairs joined by three spaces that
// describe.go documents, and checks the same "not interpreted" property
// TestValuesAreNotInterpreted checks for propertyRows.
func TestDescribe(t *testing.T) {
	entity := siren.EntityFromJSON(map[string]any{
		"properties": map[string]any{
			"state": "ok",
			"count": float64(3),
			"armed": false,
		},
	})

	// Sorted-key order: armed, count, state.
	want := "armed: false   count: 3   state: ok"
	if got := render.Describe(entity); got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
}

// TestDescribeEmpty checks the boundary case: no properties at all should
// describe as the empty string, not e.g. a lone separator.
func TestDescribeEmpty(t *testing.T) {
	if got := render.Describe(siren.Entity{}); got != "" {
		t.Errorf("Describe(Entity{}) = %q, want %q", got, "")
	}
}

func TestUnfamiliarAndMalformedDocuments(t *testing.T) {
	t.Run("an unfamiliar document still renders completely", func(t *testing.T) {
		// A document made entirely of things this app has never seen. It
		// must render completely, because "I do not recognise this" is
		// not a reason to withhold it from the person using it.
		alien := siren.EntityFromJSON(map[string]any{
			"class": []any{"quux"},
			"properties": map[string]any{
				"zork": map[string]any{
					"nested": []any{float64(1), float64(2)},
				},
			},
			"links": []any{
				map[string]any{"rel": []any{"frobnicate"}, "href": "/f"},
			},
			"actions": []any{
				map[string]any{"name": "gorp", "method": "DELETE", "href": "/g"},
			},
		})
		page := render.Document(&alien, "fallback")
		if len(page.Rows) != 3 {
			t.Fatalf("len(Rows) = %d, want 3", len(page.Rows))
		}
		if got := rowNamed(t, page, "zork").Sublabel; got != `{"nested":[1,2]}` {
			t.Errorf("Sublabel = %q, want %q", got, `{"nested":[1,2]}`)
		}
		if page.Title != "quux" {
			t.Errorf("Title = %q, want quux", page.Title)
		}
	})

	t.Run("an empty document renders no rows and uses the fallback title", func(t *testing.T) {
		empty := render.Document(&siren.Entity{}, "fallback")
		if len(empty.Rows) != 0 {
			t.Errorf("len(Rows) = %d, want 0", len(empty.Rows))
		}
		if empty.Title != "fallback" {
			t.Errorf("Title = %q, want fallback", empty.Title)
		}
	})

	t.Run("a missing document does not panic", func(t *testing.T) {
		missing := render.Document(nil, "fallback")
		if len(missing.Rows) != 0 {
			t.Errorf("len(Rows) = %d, want 0", len(missing.Rows))
		}
		if missing.Title != "fallback" {
			t.Errorf("Title = %q, want fallback", missing.Title)
		}
	})
}

// ref takes the address of a siren.Entity return value -- doc(t) and
// similar helpers return by value, and Document wants a pointer, so tests
// that call Document directly on a fresh entity need somewhere to take its
// address other than an unnamed intermediate.
func ref(entity siren.Entity) *siren.Entity {
	return &entity
}
