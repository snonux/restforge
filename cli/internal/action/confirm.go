package action

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/siren"
)

// ConfirmationText is what the user reads before confirming an unsafe
// action. A *required* checkbox's title is preferred over everything else,
// because that sentence is the server explaining the consequence -- it is
// written for exactly this moment and nothing this package could add would
// improve it. Mirrors confirmationText() in actions.js / action_service.dart:
// a checkbox that merely exists but isn't required does not stand in for
// the generic fallback sentence, the same distinction FillFields draws
// when deciding what is safe to send without asking.
func ConfirmationText(act siren.Action) string {
	for _, field := range act.Fields {
		if field.Type == "checkbox" && field.Required && field.Title != "" {
			return field.Title
		}
	}
	return fmt.Sprintf("%s?  %s to this server.", act.Label(), act.Method)
}
