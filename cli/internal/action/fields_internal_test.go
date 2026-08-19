package action

// Ported from action_service_test.dart's "fieldValues" group. That group
// calls the top-level fieldValues() function directly, straight from
// action_service_test.dart -- it lives in the same library, so nothing
// there is actually private to the test. Living in package action, not
// action_test, is what gives this file the same direct access to the
// unexported fieldValues helper in Go.

import (
	"reflect"
	"testing"

	"github.com/snonux/restforge/cli/internal/siren"
)

func fixtureCoolAction() siren.Action {
	return siren.Action{
		Name:   "cool",
		Title:  "Let the kettle cool",
		Method: "POST",
		Href:   "/cool",
		Fields: []siren.Field{
			{Name: "confirm", Type: "checkbox", Required: true, Title: "The kettle is still hot. Cool it anyway?"},
		},
	}
}

func fixtureBrewAction() siren.Action {
	return siren.Action{Name: "brew", Title: "Brew a pot of tea", Method: "POST", Href: "/brew"}
}

func TestFieldValuesChecksboxCarriesConfirmationNotAServerDefault(t *testing.T) {
	act := fixtureCoolAction()

	if got, want := fieldValues(act, true, nil, nil), (map[string]string{"confirm": "true"}); !reflect.DeepEqual(got, want) {
		t.Errorf("fieldValues(confirmed=true) = %v, want %v", got, want)
	}
	if got, want := fieldValues(act, false, nil, nil), (map[string]string{"confirm": "false"}); !reflect.DeepEqual(got, want) {
		t.Errorf("fieldValues(confirmed=false) = %v, want %v", got, want)
	}
}

func TestFieldValuesActionWithNoFieldsFillsNothing(t *testing.T) {
	act := fixtureBrewAction()

	got := fieldValues(act, true, nil, nil)
	if len(got) != 0 {
		t.Errorf("fieldValues = %v, want empty", got)
	}
}
