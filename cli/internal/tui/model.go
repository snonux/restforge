package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/session"
)

// Model is the root Bubble Tea model: one *internal/session.Session, which
// screen is current, and the state (window size, the help toggle) that
// belongs to the shell rather than to any one screen.
//
// Like every Bubble Tea Model, Model is used with value receivers by
// convention: Update takes a Model and returns a (possibly modified) copy,
// and the Bubble Tea runtime is what actually holds "the" model between
// calls. session is a pointer (Session's own methods mutate it in place; see
// internal/session's package comment on why it needs no observer of its
// own), so copying a Model never copies the session state underneath it --
// only the shell's own bookkeeping fields get the value-copy treatment
// Update's signature promises.
type Model struct {
	session *session.Session

	// base is the screen the user last explicitly navigated to: Home,
	// Document or Settings. Confirm, ValuePrompt and Detail are never
	// stored here -- they are derived from Session's own overlay state on
	// every Update/View call; see deriveScreen.
	base screen

	keys keyMap
	help help.Model

	showHelp bool
	quitting bool

	width, height int
}

// New builds the root Model wrapping sess, starting on the Home screen with
// nothing yet fetched -- opening a backend is Home's own job (task 431),
// not this shell's; see run.go's doc comment on why Run does not do it
// either.
func New(sess *session.Session) Model {
	return Model{
		session: sess,
		base:    screenHome,
		keys:    newKeyMap(),
		help:    help.New(),
	}
}

// Init has nothing to kick off: the Home screen that will load configured
// backends is a later task's Init to write, and this shell opens no
// backend and starts no fetch on its own (see New).
func (m Model) Init() tea.Cmd {
	return nil
}

// Update is Bubble Tea's single-threaded event loop entry point -- see the
// package-level async-pattern doc comment on sessionCmd for what runs here
// and what must instead run inside a returned tea.Cmd.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case sessionUpdatedMsg:
		// The tea.Cmd that just finished already mutated m.session in
		// place (see sessionCmd) -- there is nothing further to apply
		// here. Update still gets this message so Bubble Tea re-renders:
		// View always re-reads m.session fresh, the same "no cached
		// copy" contract internal/session's own package comment
		// describes for a caller of its methods.
		return m, nil
	}
	return m, nil
}

// View renders whichever screen deriveScreen picks, plus the help line
// every screen shares.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	body := m.currentScreenView()
	if m.showHelp {
		body += "\n\n" + HelpStyle.Render(m.help.FullHelpView(m.keys.FullHelp()))
	} else {
		body += "\n\n" + HelpStyle.Render(m.help.ShortHelpView(m.keys.ShortHelp()))
	}
	return AppStyle.Render(body)
}

// handleKey applies the three global bindings every screen shares -- see
// keys.go. A screen-specific key (a Confirm's yes/no, ValuePrompt's
// submit) is handled by that screen's own Update, which a later task wires
// in ahead of this fallback -- there is nothing here yet for it to fall
// past.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.Back):
		return m.handleBack(), nil
	}
	return m, nil
}

// handleBack applies the global back binding: dismiss a Detail overlay
// first (it is on top of everything else -- see deriveScreen), otherwise
// pop Session's navigation stack if there is anywhere to pop to, otherwise
// return to Home. Both DismissDetail and CanGoBack/Back are called
// directly, never through sessionCmd: none of the three perform any I/O --
// DismissDetail only clears a field already in hand, and Back only pops an
// already-fetched frame off Session's own stack (internal/nav.Nav.Back)
// rather than fetching anything -- so wrapping them in a tea.Cmd would add
// a goroutine hop for no reason. Compare Session.Refresh or Session.Activate
// on a render.FetchTarget, either of which does reach the network and so
// must go through sessionCmd.
func (m Model) handleBack() Model {
	if m.session.Detail() != nil {
		m.session.DismissDetail()
		return m
	}
	if m.session.CanGoBack() {
		m.session.Back()
		return m
	}
	m.base = screenHome
	return m
}

// currentScreen is deriveScreen applied to this Model's own session and
// base -- see deriveScreen's doc comment for the precedence it applies.
func (m Model) currentScreen() screen {
	return deriveScreen(m.session, m.base)
}

// currentScreenView renders the current screen's body. Every screen is
// still a placeholder -- see screen.go for which task fills each one in --
// so there is nothing yet to switch on; once a real screen lands, its task
// replaces this with a per-screen switch (the same closed-switch,
// default-panics convention render.RowTarget and session.SessionQuestion
// use), one case at a time.
func (m Model) currentScreenView() string {
	return renderPlaceholder(m.currentScreen())
}

// renderPlaceholder is what every screen shows until its own task lands:
// its name, styled through TitleStyle exactly as a real screen's heading
// will be, so swapping this out for the real thing changes only the body
// below the title.
func renderPlaceholder(s screen) string {
	return TitleStyle.Render(s.String()) + "\n" +
		SubtitleStyle.Render("not yet implemented")
}
