package action

import (
	"fmt"
	"strconv"

	"github.com/snonux/restforge/cli/internal/siren"
)

// FieldAnswer is a value given for one field by voice/text, matched to it
// by name. Mirrors the spoken parameter threaded through fieldValues()/
// invoke() in actions.js, and FieldAnswer in action_service.dart.
type FieldAnswer struct {
	Name string
	Text string
}

// FieldFillOutcome is what FillFields decided: either every field got a
// value, or a required one didn't and filling stopped there. A caller
// switches over this exhaustively, same as every other closed interface in
// this package -- see AskOutcome's doc comment on the default-panics-as-
// canary convention every such switch must carry.
type FieldFillOutcome interface {
	isFieldFillOutcome()
}

// FieldsFilled means every field -- required or not -- has a value, Values
// included.
type FieldsFilled struct {
	Values map[string]string
}

func (FieldsFilled) isFieldFillOutcome() {}

// FieldValueMissing means exactly one required field has neither a
// checkbox nor a default nor a matching FieldAnswer -- the one case this
// app can ask out loud for rather than refuse.
type FieldValueMissing struct {
	Field siren.Field
}

func (FieldValueMissing) isFieldFillOutcome() {}

// FieldsRefused means more than one required field is missing a value.
// Asking out loud for one field at a time is fine; dictating several is
// worse than plainly refusing, so the whole action is refused rather than
// asking for the first and silently dropping the rest.
type FieldsRefused struct {
	Reason string
}

func (FieldsRefused) isFieldFillOutcome() {}

// FillFields decides whether act's fields can all be sent as-is, whether
// exactly one required field needs asking for out loud, or whether the
// action must be refused outright (docs/DESIGN.md, "Do not invent a
// value"). Built on top of fieldValues rather than duplicating its fill
// logic: this function only adds the question "is anything required still
// missing", scanning act.Fields once more against the map fieldValues
// already produced. Mirrors the missing/problem branches of fieldValues()
// in actions.js and fillFields() in action_service.dart.
func FillFields(act siren.Action, confirmed bool, spoken *FieldAnswer) FieldFillOutcome {
	values := fieldValues(act, confirmed, spoken)

	missing, refused := firstMissingRequiredField(act, values)
	if refused != "" {
		return FieldsRefused{Reason: refused}
	}
	if missing != nil {
		return FieldValueMissing{Field: *missing}
	}
	return FieldsFilled{Values: values}
}

// fieldValues fills an action's fields generically. Nothing here knows
// what any field means:
//
//   - a checkbox carries the user's confirmation, because a required
//     checkbox *is* the confirmation this app already asked for;
//   - a value matching spoken's field name is what the user just said,
//     asked for by FillFields on an earlier call;
//   - anything else takes the default the server declared in Value.
//
// A required field with none of those is left out of the result here --
// this function only fills what it safely can. Deciding whether that
// omission means asking out loud or refusing the action is FillFields's
// job, not this one, so a direct caller always gets a plain map back.
// Mirrors the field-filling loop in fieldValues() in actions.js /
// action_service.dart. Unexported: nothing outside this package needs the
// bare fill without FillFields's missing-field decision layered on top --
// tests reach it directly the same way action_service_test.dart's
// "fieldValues" group does, by living in this package.
func fieldValues(act siren.Action, confirmed bool, spoken *FieldAnswer) map[string]string {
	values := make(map[string]string)
	for _, field := range act.Fields {
		if field.Name == "" {
			continue
		}
		switch {
		case field.Type == "checkbox":
			values[field.Name] = strconv.FormatBool(confirmed)
		case spoken != nil && spoken.Name == field.Name:
			values[field.Name] = spoken.Text
		case field.Value != nil:
			values[field.Name] = fmt.Sprint(field.Value)
		}
	}
	return values
}

// firstMissingRequiredField scans act.Fields for required fields absent
// from values. It returns the first one found, or a non-empty refusal
// reason once a second one turns up -- split out of FillFields so that
// function stays the shape of "fill, then decide", matching the ~30-line
// function guideline this codebase holds itself to.
func firstMissingRequiredField(act siren.Action, values map[string]string) (missing *siren.Field, refusalReason string) {
	for _, field := range act.Fields {
		if field.Name == "" || !field.Required {
			continue
		}
		if _, ok := values[field.Name]; ok {
			continue
		}
		if missing == nil {
			// Asked for by voice rather than refused -- but only the
			// first one.
			f := field
			missing = &f
			continue
		}
		return missing, "needs values for more than one field, which cannot be asked for one at a time"
	}
	return missing, ""
}
