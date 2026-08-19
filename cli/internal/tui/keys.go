package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// keyMap is the global key bindings every screen shares, regardless of
// which one is current: quit, back and help, exactly the three the tui
// shell task names. A screen-specific binding (yes/no on Confirm, submit on
// ValuePrompt, a list's up/down) is that screen's own task to add -- this
// shell only owns the bindings that must work identically everywhere.
type keyMap struct {
	Quit key.Binding
	Back key.Binding
	Help key.Binding
}

// keyMap satisfies bubbles/help's help.KeyMap structurally (see ShortHelp/
// FullHelp below); asserted here so a signature change to either method
// breaks the build right here, not wherever Model.View calls help.Model.
var _ help.KeyMap = keyMap{}

// newKeyMap builds the global key map with its bound keys and the help
// text bubbles/help renders for them.
func newKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "backspace"),
			key.WithHelp("esc/h", "back"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
	}
}

// ShortHelp and FullHelp implement bubbles/help's help.KeyMap, so
// Model.View can render these bindings through a help.Model rather than
// hand-formatting a hint line. Both return the same three bindings: there
// is nothing here yet a "short" summary would need to abbreviate. A later
// screen task with its own bindings (Confirm's yes/no, and so on) composes
// its own key map alongside this one rather than extending it -- see
// keys.go's own doc comment on why the global set stays exactly these
// three.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Back, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
