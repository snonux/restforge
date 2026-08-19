// Port of flutter/test/models/siren_test.dart and pebble/tools/test-siren.js
// as Go table-driven tests. See AGENTS.md section 5 ("Test style") and this
// package's doc comment for why: an unknown class, rel, action name or
// field type must come back as "not offered", never as an error, so most
// of what follows is malformed or unfamiliar input that must never panic.
//
// Sample rels, classes, action names and property names below are
// deliberately made up (never a real API's vocabulary), the same way the
// fixture in both source test files is.
package siren_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/siren"
)

// docJSON mirrors the DOC/docJson fixture shared by both source test
// files.
var docJSON = map[string]any{
	"class":      []any{"home"},
	"title":      "Example service",
	"properties": map[string]any{"apiVersion": float64(1), "version": "v1.2.3"},
	"entities": []any{
		map[string]any{
			"class":      []any{"widget"},
			"properties": map[string]any{"name": "a"},
		},
		map[string]any{
			"class": []any{"widget"},
			"href":  "/widgets/b",
		},
	},
	"links": []any{
		map[string]any{"rel": []any{"self"}, "href": "/"},
		map[string]any{"rel": []any{"collection", "widgets"}, "href": "/widgets"},
	},
	"actions": []any{
		map[string]any{
			"name": "restart", "method": "POST", "href": "/restart",
			"title": "Restart the service",
			"fields": []any{
				map[string]any{"name": "force", "type": "checkbox", "title": "Really?"},
			},
		},
		map[string]any{"name": "peek", "href": "/peek"},
	},
}

func doc(t *testing.T) siren.Entity {
	t.Helper()
	return siren.EntityFromJSON(docJSON)
}

func TestAccessors(t *testing.T) {
	d := doc(t)
	if got := d.Properties["version"]; got != "v1.2.3" {
		t.Errorf("Properties[version] = %v, want v1.2.3", got)
	}
	if got := d.Classes[0]; got != "home" {
		t.Errorf("Classes[0] = %q, want home", got)
	}
	if len(d.Links) != 2 {
		t.Errorf("len(Links) = %d, want 2", len(d.Links))
	}
	if len(d.Actions) != 2 {
		t.Errorf("len(Actions) = %d, want 2", len(d.Actions))
	}
	if len(d.Entities) != 2 {
		t.Errorf("len(Entities) = %d, want 2", len(d.Entities))
	}
}

func TestLookup(t *testing.T) {
	d := doc(t)

	if got := d.Follow("self"); got != "/" {
		t.Errorf("Follow(self) = %q, want /", got)
	}
	// Siren's rel is a list; a link is found by any of its rels, not just
	// the first, because which one a server puts first is not part of the
	// contract.
	if got := d.Follow("widgets"); got != "/widgets" {
		t.Errorf("Follow(widgets) = %q, want /widgets", got)
	}
	if got := d.Follow("nothing-like-this"); got != "" {
		t.Errorf("Follow(nothing-like-this) = %q, want \"\"", got)
	}

	restart := d.ActionByName("restart")
	if restart == nil || restart.Href != "/restart" {
		t.Errorf("ActionByName(restart).Href = %v, want /restart", restart)
	}
	if got := d.ActionByName("never-offered"); got != nil {
		t.Errorf("ActionByName(never-offered) = %v, want nil", got)
	}
	if len(restart.Fields) != 1 {
		t.Errorf("restart Fields = %d, want 1", len(restart.Fields))
	}

	peek := d.ActionByName("peek")
	if peek == nil || len(peek.Fields) != 0 {
		t.Errorf("peek Fields = %v, want empty", peek)
	}

	field := restart.Fields[0]
	if field.Required {
		t.Error("force field Required = true, want false: docJSON's \"force\" field carries no \"required\" member")
	}

	withRequired := siren.EntityFromJSON(map[string]any{
		"actions": []any{
			map[string]any{
				"name": "label",
				"href": "/label",
				"fields": []any{
					map[string]any{"name": "text", "type": "text", "required": true},
				},
			},
		},
	})
	if got := withRequired.ActionByName("label").Fields[0].Required; !got {
		t.Error("field marked required in JSON did not report Required = true")
	}
}

func TestMethod(t *testing.T) {
	d := doc(t)
	if got := d.ActionByName("restart").Method; got != "POST" {
		t.Errorf("restart Method = %q, want POST", got)
	}
	// Siren's default. A server that means to change something says so.
	if got := d.ActionByName("peek").Method; got != "GET" {
		t.Errorf("peek Method = %q, want GET", got)
	}
}

func TestReferences(t *testing.T) {
	d := doc(t)
	if d.Entities[0].IsReference {
		t.Error("embedded entity IsReference = true, want false")
	}
	if !d.Entities[1].IsReference {
		t.Error("bare-href entity IsReference = false, want true")
	}
}

func TestLabel(t *testing.T) {
	d := doc(t)
	if got := d.Label(); got != "Example service" {
		t.Errorf("doc Label() = %q, want %q", got, "Example service")
	}

	if got := siren.ActionFromJSON(map[string]any{"name": "restart"}).Label(); got != "restart" {
		t.Errorf("Action Label() = %q, want restart", got)
	}

	// Found against a real API: entities sharing one class rendered as
	// identical rows, because the only thing distinguishing them was a
	// property. A list where every row reads the same is not a list.
	identifying := siren.EntityFromJSON(map[string]any{
		"class":      []any{"host", "f"},
		"properties": map[string]any{"address": "10.0.0.1", "name": "f0"},
	})
	if got := identifying.Label(); got != "f0" {
		t.Errorf("identifying-property Label() = %q, want f0", got)
	}

	numeric := siren.EntityFromJSON(map[string]any{
		"class":      []any{"ticket"},
		"properties": map[string]any{"id": float64(7)},
	})
	if got := numeric.Label(); got != "7" {
		t.Errorf("numeric-identifier Label() = %q, want 7", got)
	}

	named := siren.EntityFromJSON(map[string]any{
		"properties": map[string]any{"address": "x", "name": "f0"},
	})
	if got := named.Identifier(); got != "name" {
		t.Errorf("Identifier() = %q, want name", got)
	}

	unidentified := siren.EntityFromJSON(map[string]any{
		"properties": map[string]any{"address": "x"},
	})
	if got := unidentified.Identifier(); got != "" {
		t.Errorf("Identifier() = %q, want \"\"", got)
	}

	titleWins := siren.EntityFromJSON(map[string]any{
		"title":      "Top shelf",
		"properties": map[string]any{"name": "top"},
	})
	if got := titleWins.Label(); got != "Top shelf" {
		t.Errorf("title-over-property Label() = %q, want %q", got, "Top shelf")
	}

	classed := siren.EntityFromJSON(map[string]any{
		"class": []any{"widget", "large"},
	})
	if got := classed.Label(); got != "widget large" {
		t.Errorf("class Label() = %q, want %q", got, "widget large")
	}

	relOnly := siren.LinkFromJSON(map[string]any{"rel": []any{"collection"}})
	if got := relOnly.Label(); got != "collection" {
		t.Errorf("rel-only Link Label() = %q, want collection", got)
	}

	// Never invent a name: an invented name is a claim about what the
	// thing is, and this client has no basis for one.
	if got := siren.EntityFromJSON(map[string]any{}).Label(); got != "" {
		t.Errorf("empty Entity Label() = %q, want \"\"", got)
	}
	if got := siren.EntityFromJSON(nil).Label(); got != "" {
		t.Errorf("nil Entity Label() = %q, want \"\"", got)
	}
}

func TestVersionCheck(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
		wantEmpty  bool
		wantSubstr string
	}{
		{"supported version proceeds", map[string]any{"apiVersion": float64(1)}, true, ""},
		{"older version proceeds", map[string]any{"apiVersion": float64(0)}, true, ""},
		{"newer version stops", map[string]any{"apiVersion": float64(2)}, false, "apiVersion 2"},
		// A server that declares nothing is making no claim this client
		// can act on, and refusing to proceed would break it against every
		// server that never had the field.
		{"absent version proceeds", map[string]any{}, true, ""},
		{"non-numeric version proceeds", map[string]any{"apiVersion": "one"}, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := siren.EntityFromJSON(map[string]any{"properties": tt.properties})
			got := e.VersionProblem()
			if tt.wantEmpty && got != "" {
				t.Errorf("VersionProblem() = %q, want \"\"", got)
			}
			if tt.wantSubstr != "" && !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("VersionProblem() = %q, want substring %q", got, tt.wantSubstr)
			}
		})
	}
}

// TestMalformedInputNeverPanics is the important half: nothing below is
// well-formed, and none of it may panic.
func TestMalformedInputNeverPanics(t *testing.T) {
	junk := []any{
		nil,
		map[string]any{},
		[]any{},
		"a string",
		42,
		map[string]any{
			"links": "not a list", "actions": nil, "entities": float64(7),
			"properties": []any{},
		},
		map[string]any{
			"links": []any{map[string]any{"rel": "self", "href": "/"}},
		},
		map[string]any{
			"links": []any{map[string]any{"rel": []any{"self"}}},
		},
		map[string]any{
			"actions": []any{map[string]any{}},
		},
	}

	for i, input := range junk {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("input %d: panicked: %v", i, r)
				}
			}()
			e := siren.EntityFromJSON(input)
			_ = e.Classes
			_ = e.Properties
			_ = e.Links
			_ = e.Actions
			_ = e.Entities
			_ = e.Follow("self")
			_ = e.ActionByName("restart")
			_ = e.Label()
			_ = e.VersionProblem()
			_ = e.Summary()
		}()
	}
}

func TestStringRelIsStillMatched(t *testing.T) {
	// A lenient server may send a bare string where Siren specifies a
	// list.
	e := siren.EntityFromJSON(map[string]any{
		"links": []any{map[string]any{"rel": "self", "href": "/"}},
	})
	if got := e.Follow("self"); got != "/" {
		t.Errorf("Follow(self) = %q, want /", got)
	}
}

func TestLinkWithoutHrefIsNotOffered(t *testing.T) {
	// A link with no href cannot be followed, so it is not a link.
	e := siren.EntityFromJSON(map[string]any{
		"links": []any{map[string]any{"rel": []any{"self"}}},
	})
	if got := e.Follow("self"); got != "" {
		t.Errorf("Follow(self) = %q, want \"\"", got)
	}
}

func TestSummary(t *testing.T) {
	d := doc(t)
	text := d.Summary()
	for _, want := range []string{"2 link(s)", "2 action(s)", "2 entity(ies)"} {
		if !strings.Contains(text, want) {
			t.Errorf("Summary() = %q, want substring %q", text, want)
		}
	}
	// Counts, not names: the summary has to stay useful against a server
	// this client has never seen. The class is the one exception -- it is
	// shown, not interpreted.
	if !strings.Contains(text, "class [home]") {
		t.Errorf("Summary() = %q, want substring %q", text, "class [home]")
	}
}

// TestParseDocument covers ParseDocument, which is new relative to
// siren.js: siren.js is only ever handed an already-decoded object
// (http.js owns JSON.parse there). Here the JSON-text boundary lives in
// this package instead, so its never-panic guarantee is tested at that
// boundary too.
func TestParseDocument(t *testing.T) {
	t.Run("well-formed document parses", func(t *testing.T) {
		body, err := json.Marshal(docJSON)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		entity, err := siren.ParseDocument(body)
		if err != nil {
			t.Fatalf("ParseDocument() error = %v, want nil", err)
		}
		if got := entity.Label(); got != "Example service" {
			t.Errorf("Label() = %q, want %q", got, "Example service")
		}
		if len(entity.Links) != 2 {
			t.Errorf("len(Links) = %d, want 2", len(entity.Links))
		}
	})

	t.Run("invalid JSON syntax is a Parse failure, not a panic", func(t *testing.T) {
		_, err := siren.ParseDocument([]byte("{not valid json"))
		if err == nil {
			t.Fatal("ParseDocument() error = nil, want a *failure.Failure")
		}
		var f *failure.Failure
		if !errors.As(err, &f) {
			t.Fatalf("error is not *failure.Failure: %v", err)
		}
		if f.Kind != failure.Parse {
			t.Errorf("Kind = %v, want %v", f.Kind, failure.Parse)
		}
	})

	t.Run("a JSON value that is not an object is a Parse failure", func(t *testing.T) {
		for _, body := range []string{"[1,2,3]", `"just a string"`, "42", "null"} {
			_, err := siren.ParseDocument([]byte(body))
			if err == nil {
				t.Fatalf("body %q: error = nil, want a *failure.Failure", body)
			}
			var f *failure.Failure
			if !errors.As(err, &f) {
				t.Fatalf("body %q: error is not *failure.Failure: %v", body, err)
			}
			if f.Kind != failure.Parse {
				t.Errorf("body %q: Kind = %v, want %v", body, f.Kind, failure.Parse)
			}
		}
	})

	t.Run("garbage bytes never panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked: %v", r)
			}
		}()
		if _, err := siren.ParseDocument([]byte("not json at all, just noise {{{")); err == nil {
			t.Fatal("ParseDocument() error = nil, want a *failure.Failure")
		}
	})
}
