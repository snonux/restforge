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

	// home is the Home screen's own state -- the configured-backend and
	// saved-shortcut lists (see home.go). Always kept up to date, even
	// while a different screen is current, the same way base only changes
	// on explicit navigation: there is exactly one homeModel for the
	// program's lifetime, not one per visit.
	home homeModel

	// document is the Document screen's own state -- the row list built
	// from Session.Document (see document.go). Resynced from Session
	// whenever Update handles a message that may have changed it
	// (sessionUpdatedMsg, homeOpenedMsg, homeQuickRanMsg, the global Back
	// key) -- see each of those cases below and documentModel.syncRows's
	// own doc comment for why that resync is cheap to call unconditionally.
	document documentModel

	// settings is the Settings screen's own state -- the working backend
	// list plus whichever of its two modes is current (settings.go). Unlike
	// home and document, it is only ever meaningfully populated once the
	// user actually opens Settings (see home_update.go's openSettings,
	// wired to homeSettingsBinding): there is nothing for it to resync from
	// on every Update the way documentModel resyncs from Session, since
	// Settings has no equivalent of Session to resync from -- it edits
	// internal/config directly, seeded from Home's own list at the moment
	// it opens.
	settings settingsModel

	showHelp bool
	quitting bool

	width, height int
}

// New builds the root Model wrapping sess, starting on the Home screen with
// nothing yet fetched -- opening a backend (or running a saved shortcut) is
// Home's own job, kicked off from Init below once the terminal is running,
// not this constructor's; see run.go's doc comment on why Run does not do
// it either.
func New(sess *session.Session) Model {
	return Model{
		session:  sess,
		base:     screenHome,
		keys:     newKeyMap(),
		help:     help.New(),
		home:     newHomeModel(),
		document: newDocumentModel(),
		settings: newSettingsModel(nil),
	}
}

// Init kicks off Home's own load of the configured backends and saved
// shortcuts (homeInitCmd, home_cmd.go) -- internal/config and internal/quick
// directly, not through Session; see homeInitCmd's own doc comment for why.
// No backend is opened and no fetch is started here: deciding which
// backend to open, or which shortcut to run, only happens once the user
// picks one on the rendered Home screen (see home_update.go's
// activateHomeSelection).
func (m Model) Init() tea.Cmd {
	return homeInitCmd()
}

// Update is Bubble Tea's single-threaded event loop entry point -- see the
// package-level async-pattern doc comment on sessionCmd for what runs here
// and what must instead run inside a returned tea.Cmd.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		m.home = m.home.resize(msg.Width, msg.Height)
		m.document = m.document.resize(msg.Width, msg.Height)
		m.settings = m.settings.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case sessionUpdatedMsg:
		// The tea.Cmd that just finished already mutated m.session in
		// place (see sessionCmd) -- there is nothing further to apply
		// here beyond resyncing the Document screen's own row list (see
		// documentModel.syncRows's own doc comment) from whatever Session
		// now holds. Update still gets this message so Bubble Tea
		// re-renders: View always re-reads m.session fresh, the same "no
		// cached copy" contract internal/session's own package comment
		// describes for a caller of its methods.
		m.document = m.document.syncRows(m.session.Document())
		return m, nil
	case homeLoadedMsg:
		m.home = m.home.applyLoaded(msg)
		return m, nil
	case homeOpenedMsg:
		m.base = screenDocument
		m.document = m.document.syncRows(m.session.Document())
		return m, nil
	case homeQuickRanMsg:
		m = m.applyQuickRan(msg)
		m.document = m.document.syncRows(m.session.Document())
		return m, nil
	case settingsSavedMsg:
		return m.applySettingsSaved(msg)
	}
	return m, nil
}

// applySettingsSaved folds a settingsSavedMsg into the shell: a save failure
// stays on the Settings screen with the error reported as its own notice
// (settingsModel.notice), so the user can retry without losing the working
// list; success returns to Home and re-runs homeInitCmd -- a full reload
// from internal/config and internal/quick, exactly what Model.Init runs at
// startup -- rather than hand-rolling a narrower "just the backends changed"
// update, since a backend rename or removal can also change which backend a
// saved shortcut now resolves to (see loadQuickRows, home_cmd.go).
func (m Model) applySettingsSaved(msg settingsSavedMsg) (tea.Model, tea.Cmd) {
	m.settings.saving = false
	if msg.err != nil {
		m.settings.notice = "could not save: " + msg.err.Error()
		return m, nil
	}
	m.settings.notice = ""
	m.base = screenHome
	return m, homeInitCmd()
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
// keys.go -- before falling back to whichever screen is current. Home
// (updateHome, home_update.go) and Document (updateDocument,
// document_update.go) are the two screens with their own key handling so
// far; a screen-specific key for one of the screens still pending
// (Confirm's yes/no, ValuePrompt's submit, and so on) is that screen's own
// task to add to this same fallback.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.Back):
		m = m.handleBack()
		// handleBack may have popped Session's own navigation stack (or
		// dismissed Detail, once task 731 sets it) -- resync the Document
		// screen's row list from whatever Session shows now, the same
		// call every other Session-mutating branch in Update makes. See
		// documentModel.syncRows's own doc comment for why this is cheap
		// even when nothing actually changed (Detail dismissed, or
		// nothing left to pop).
		m.document = m.document.syncRows(m.session.Document())
		return m, nil
	}
	switch m.currentScreen() {
	case screenHome:
		return m.updateHome(msg)
	case screenDocument:
		return m.updateDocument(msg)
	case screenSettings:
		return m.updateSettings(msg)
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
	if m.currentScreen() == screenSettings && m.settings.mode == settingsModeEdit {
		// One level down, not all the way to Home: settingsModeEdit is its
		// own sub-state within screenSettings (settings.go), the same way
		// Detail layers over whatever base screen was current -- see
		// settings_update.go's updateSettings for why this step never
		// reaches updateSettingsEdit itself.
		m.settings = m.settings.cancelEdit()
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

// currentScreenView renders the current screen's body: Home's own view
// (home.go), Document's own view (document.go) and Settings' own view
// (settings.go) once one of them is current, renderPlaceholder for every
// screen still pending -- see screen.go for which task fills each one in.
// Grown one case at a time as each screen task lands, rather than a closed
// switch with a default-panics canary (render.RowTarget and
// session.SessionQuestion's own convention): unlike those, "not yet
// implemented" is this switch's deliberate, temporary default for Confirm,
// ValuePrompt and Detail, not a bug.
func (m Model) currentScreenView() string {
	switch m.currentScreen() {
	case screenHome:
		return m.home.View()
	case screenDocument:
		return m.document.View(m.session)
	case screenSettings:
		return m.settings.View()
	}
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
