package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/backend"
)

// Field indices into settingsForm.inputs -- named rather than left as bare
// ints so startEdit/startAdd/confirmEdit (settings_update.go) and editView
// below read as "the name field", not "inputs[0]".
const (
	fieldName = iota
	fieldBaseURL
	fieldAuthHeader
	fieldSecret
	fieldStartRel
	fieldCount
)

// settingsForm is the five-field edit form -- one bubbles/textinput.Model
// per backend.Backend field, plus which one currently has focus. Kept as its
// own type (rather than five named fields directly on settingsModel) so
// focusNext/focusPrev/blur below can index into it uniformly instead of a
// five-way switch.
//
// secretOriginal is never loaded into inputs[fieldSecret] -- see
// settings_screen.dart's own module comment (settings_screen.dart, top of
// file) on why an existing secret is kept out of a text field in the Flutter
// port already, for the same reason repeated here: this screen's state is
// just as inspectable as that widget tree (a future test dumping
// settingsModel, a debugger). startEdit (settings_update.go) leaves
// inputs[fieldSecret] blank and records the stored secret in secretOriginal
// instead; confirmEdit substitutes it back in only when the field was left
// blank -- typing into it replaces it, exactly like the Dart original's
// _BackendRow.toRawMap.
type settingsForm struct {
	inputs         [fieldCount]textinput.Model
	focus          int
	secretOriginal string
}

// newSettingsForm builds all five inputs, unfocused -- startEdit/startAdd
// (settings_update.go) populate values and call focusField(fieldName) once
// the form's contents are decided.
func newSettingsForm() settingsForm {
	f := settingsForm{}
	labels := [fieldCount]string{"Name", "Base URL", "Auth header", "Secret", "Start at rel"}
	for i := range f.inputs {
		f.inputs[i] = textinput.New()
		f.inputs[i].Prompt = labels[i] + ": "
		f.inputs[i].CharLimit = backend.MaxFieldLength
	}
	// EchoPassword masks the secret as it is typed -- the terminal
	// equivalent of settings_screen.dart's TextField(obscureText: true) on
	// the same field. Never EchoNormal: a secret must not render in plain
	// view any more here than it may be logged (internal/backend's own
	// package comment).
	f.inputs[fieldSecret].EchoMode = textinput.EchoPassword
	return f
}

// focusField blurs whichever input currently has focus and focuses index,
// returning the tea.Cmd Focus produces (cursor.Model's blink command) so the
// caller (updateSettingsEdit, settings_update.go) can return it from Update
// rather than drop it -- the one piece of I/O-shaped behaviour a textinput
// needs threaded through, everything else about focus switching here is
// plain state.
func (f settingsForm) focusField(index int) (settingsForm, tea.Cmd) {
	f.inputs[f.focus].Blur()
	f.focus = index
	cmd := f.inputs[f.focus].Focus()
	return f, cmd
}

// values reads the five fields back out as raw strings, substituting
// secretOriginal for the secret field when it was left blank -- see
// settingsForm's own doc comment on why. Feeds directly into
// backend.Normalise in confirmEdit (settings_update.go).
func (f settingsForm) values() backend.Backend {
	secret := f.inputs[fieldSecret].Value()
	if secret == "" {
		secret = f.secretOriginal
	}
	return backend.Backend{
		Name:       f.inputs[fieldName].Value(),
		BaseURL:    f.inputs[fieldBaseURL].Value(),
		AuthHeader: f.inputs[fieldAuthHeader].Value(),
		Secret:     secret,
		StartRel:   f.inputs[fieldStartRel].Value(),
	}
}

// populate loads b's fields into the form for editing an existing backend --
// every field except Secret, which stays blank with b.Secret recorded in
// secretOriginal instead (see settingsForm's own doc comment).
func (f settingsForm) populate(b backend.Backend) settingsForm {
	f.inputs[fieldName].SetValue(b.Name)
	f.inputs[fieldBaseURL].SetValue(b.BaseURL)
	f.inputs[fieldAuthHeader].SetValue(b.AuthHeader)
	f.inputs[fieldSecret].SetValue("")
	f.inputs[fieldStartRel].SetValue(b.StartRel)
	f.secretOriginal = b.Secret
	return f
}

// blank resets the form for adding a new backend -- every field empty
// except AuthHeader, pre-filled with backend.DefaultAuthHeader. Mirrors
// _BackendRow.blank's own comment in settings_screen.dart: there is nothing
// secret about the default header name, so pre-filling it costs nothing and
// saves a near-universal retype.
func (f settingsForm) blank() settingsForm {
	f.inputs[fieldName].SetValue("")
	f.inputs[fieldBaseURL].SetValue("")
	f.inputs[fieldAuthHeader].SetValue(backend.DefaultAuthHeader)
	f.inputs[fieldSecret].SetValue("")
	f.inputs[fieldStartRel].SetValue("")
	f.secretOriginal = ""
	return f
}

// secretHelp is the secret field's own helper line, switched on whether an
// existing secret is being kept -- mirrors the Dart card's own
// InputDecoration.helperText for the same field.
func (f settingsForm) secretHelp() string {
	if f.secretOriginal == "" {
		return "Kept on this device."
	}
	return "Leave blank to keep the existing secret."
}

// editView renders settingsModeEdit: a heading naming whether this is an add
// or an edit, all five fields with their own helper text, any validation
// error from the last confirm attempt, and a hint line.
func (s settingsModel) editView() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render(s.editHeading()) + "\n\n")

	helps := [fieldCount]string{
		"Shown on the backend picker",
		"The Siren API root. A trailing / is added if you omit it.",
		"The secret is sent in this header, never in the URL.",
		s.form.secretHelp(),
		"Optional. A link rel to open straight after the root.",
	}
	for i, in := range s.form.inputs {
		b.WriteString(in.View() + "\n")
		b.WriteString(MutedStyle.Render("  "+helps[i]) + "\n")
	}

	if s.editError != "" {
		b.WriteString(ErrorStyle.Render(s.editError) + "\n")
	}
	b.WriteString("\n" + MutedStyle.Render("tab/↑/↓ move field · enter confirm · esc cancel"))
	return b.String()
}

// editHeading names whether the form is adding a new backend or editing an
// existing one -- editIndex < 0 is startAdd's own marker (settingsModel's
// own doc comment).
func (s settingsModel) editHeading() string {
	if s.editIndex < 0 {
		return "Add backend"
	}
	return "Edit backend"
}
