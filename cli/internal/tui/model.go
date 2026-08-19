package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
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

	// valuePrompt is the ValuePrompt screen's own state -- the one
	// bubbles/textinput.Model a session.ValueQuestion needs (valueprompt.go).
	// Resynced from Session.Question() everywhere documentModel resyncs from
	// Session.Document() (see valuePromptModel.syncFromSession's own doc
	// comment for why): a ConfirmQuestion carries nothing to hold between
	// renders, so it has no equivalent field here -- see confirm.go's own
	// doc comment.
	valuePrompt valuePromptModel

	// detail is the Detail screen's own state -- the one bubbles/
	// viewport.Model a session.DetailView needs (detail.go). Resynced from
	// Session.Detail() everywhere valuePrompt resyncs from Session.Question()
	// -- see detailModel.syncFromSession's own doc comment for why.
	detail detailModel

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
		session:     sess,
		base:        screenHome,
		keys:        newKeyMap(),
		help:        help.New(),
		home:        newHomeModel(),
		document:    newDocumentModel(),
		settings:    newSettingsModel(nil),
		valuePrompt: newValuePromptModel(),
		detail:      newDetailModel(),
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
		return m.applyWindowSize(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case liveTickMsg:
		// A live watch's poll timer fired: internal/live's createTimer
		// (live_cmd.go) posted this instead of invoking the poll callback on
		// the timer's own goroutine, so the poll -- and the Session mutation
		// it performs through internal/live's Handlers -- runs inside the
		// sessionCmd handleLiveTick wraps it in, on a goroutine Update
		// scheduled, never concurrently with this loop. See live_cmd.go's
		// own package comment for the hazard this closes.
		return m.handleLiveTick(msg)
	case spinner.TickMsg:
		// The live-progress spinner's own tick loop -- see
		// startSpinnerCmd/handleSpinnerTick (live_cmd.go). Routed here rather
		// than handled inside documentModel because TickMsg is a Bubble Tea
		// message, not a Document-screen-local one, and the loop's IsLive
		// gate reads Session at the Model level the same way every other
		// resync does.
		return m.handleSpinnerTick(msg)
	case sessionUpdatedMsg:
		// The tea.Cmd that just finished already mutated m.session in
		// place (see sessionCmd) -- there is nothing further to apply
		// here beyond resyncing the Document screen from whatever Session
		// now holds. Update still gets this message so Bubble Tea
		// re-renders: View always re-reads m.session fresh, the same "no
		// cached copy" contract internal/session's own package comment
		// describes for a caller of its methods.
		return m.resyncDocumentScreen(), m.startSpinnerCmd()
	case homeLoadedMsg:
		m.home = m.home.applyLoaded(msg)
		return m, nil
	case homeOpenedMsg:
		m.base = screenDocument
		return m.resyncDocumentScreen(), m.startSpinnerCmd()
	case homeQuickRanMsg:
		m = m.applyQuickRan(msg)
		// RunQuick (session/quick.go) may already have called askAction for
		// an action shortcut, synchronously setting Session.Question() to a
		// ConfirmQuestion or ValueQuestion before this message ever arrives
		// -- resync the same way sessionUpdatedMsg does, so a shortcut gets
		// exactly the confirmation flow a hand-reached action would.
		return m.resyncDocumentScreen(), m.startSpinnerCmd()
	case settingsSavedMsg:
		return m.applySettingsSaved(msg)
	case homeQuickDeletedMsg:
		return m.applyQuickDeleted(msg)
	case documentQuickSavedMsg:
		return m.applyQuickSaved(msg)
	case list.FilterMatchesMsg:
		// list.Model's own filterItems debounces recomputing the match set
		// away from every keystroke: typing a query returns a tea.Cmd that
		// lands back here, at this package's own Update, some tens of
		// milliseconds later -- not synchronously inside list.Model's own
		// Update call the way a first glance at vikeys.go's filter handling
		// might suggest. Without this case the message has nowhere to go
		// (Bubble Tea does not know it belongs to a nested list.Model), so
		// list.Model.filteredItems is never actually updated and every
		// screen's own filter looks like it does nothing -- see
		// applyFilterMatches's own doc comment for where it is routed.
		return m.applyFilterMatches(msg)
	}
	return m, nil
}

// applyFilterMatches forwards msg to whichever screen-owned list.Model is
// current -- Home's focused list, Document's row list, or Settings' backend
// list while settingsModeList is current -- mirroring currentFilterState's
// own routing (vikeys.go), since a FilterMatchesMsg only ever means
// anything to the list that started the filterItems cmd producing it. A
// message that lands after the user has navigated away from that list
// (Confirm/ValuePrompt/Detail/Settings' own edit mode all have none) is
// simply dropped -- there is nowhere for it to go and nothing on screen it
// could affect.
func (m Model) applyFilterMatches(msg list.FilterMatchesMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.currentScreen() {
	case screenHome:
		switch m.home.focus {
		case homeFocusBackends:
			m.home.backends, cmd = m.home.backends.Update(msg)
		case homeFocusQuick:
			m.home.shortcuts, cmd = m.home.shortcuts.Update(msg)
		}
	case screenDocument:
		m.document.rows, cmd = m.document.rows.Update(msg)
	case screenSettings:
		if m.settings.mode == settingsModeList {
			m.settings.list, cmd = m.settings.list.Update(msg)
		}
	}
	return m, cmd
}

// applyWindowSize forwards a tea.WindowSizeMsg to every screen's own
// resize and records the new terminal size for View -- the one Update case
// that is pure forwarding, split out so Update's switch stays a short
// dispatcher.
func (m Model) applyWindowSize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	m.help.Width = msg.Width
	m.home = m.home.resize(msg.Width, msg.Height)
	m.document = m.document.resize(msg.Width, msg.Height)
	m.settings = m.settings.resize(msg.Width, msg.Height)
	m.detail = m.detail.resize(msg.Width, msg.Height)
	return m
}

// resyncDocumentScreen rebuilds the Document screen's own state from
// Session after a Session-mutating step -- the shared body of the
// sessionUpdatedMsg, homeOpenedMsg and homeQuickRanMsg cases, which all
// leave Session in a state the Document screen must re-read. Returns m with
// the row list (documentModel.syncRows -- a no-op when nothing changed), the
// value-prompt overlay and the detail overlay resynced. Callers pair this
// with startSpinnerCmd to (re)start the live-progress spinner when the
// update left Session.IsLive true; it is a no-op otherwise.
func (m Model) resyncDocumentScreen() Model {
	m.document = m.document.syncRows(m.session.Document())
	m.valuePrompt = m.valuePrompt.syncFromSession(m.session.Question())
	m.detail = m.detail.syncFromSession(m.session.Detail())
	return m
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
// keys.go -- and the vi h/l remap (vikeys.go), before falling back to
// whichever screen is current, unless a screen-owned list.Model's own
// filter input has first refusal instead -- see currentFilterState's own
// doc comment (vikeys.go) for the two states this checks and why:
//
//   - list.Filtering (a filter query is actively being typed): every key
//     belongs to list.Model's own FilterInput, not the shell's global
//     bindings, the vi remap, or this screen's own Enter/d/s/tab overrides
//     (updateHome/updateDocument/updateSettingsList each make the same
//     check for the same reason) -- dispatched straight through,
//     unmodified.
//   - Otherwise, but the filter is still applied (list.FilterApplied): only
//     Esc defers to the list -- list.Model's own ClearFilter binding, which
//     would otherwise lose to the shell's global Back the same way it would
//     during Filtering. Every other key (q, ?, h/l/j/k, this screen's own
//     Enter/d/s) behaves normally, since list.Model's handleBrowsing does
//     not intercept any of those while a filter is merely applied rather
//     than being edited.
//
// Home (updateHome, home_update.go), Document (updateDocument,
// document_update.go), Settings (updateSettings, settings_update.go),
// Confirm (updateConfirm, confirm_update.go), ValuePrompt
// (updateValuePrompt, valueprompt_update.go) and Detail (updateDetail,
// detail_update.go) are the screens with their own key handling.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch state := m.currentFilterState(); {
	case state == list.Filtering:
		return m.dispatchToScreen(msg)
	case state != list.Unfiltered && key.Matches(msg, m.keys.Back):
		return m.dispatchToScreen(msg)
	}
	msg = m.remapViKey(msg)
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.Back):
		m = m.handleBack()
		// handleBack may have popped Session's own navigation stack,
		// dismissed Detail or declined a pending question -- resync the
		// Document screen's row list, the ValuePrompt screen's input and
		// the Detail screen's viewport from whatever Session shows now,
		// the same call every other Session-mutating branch in Update
		// makes. See documentModel.syncRows's own doc comment for why
		// this is cheap even when nothing actually changed.
		m.document = m.document.syncRows(m.session.Document())
		m.valuePrompt = m.valuePrompt.syncFromSession(m.session.Question())
		m.detail = m.detail.syncFromSession(m.session.Detail())
		return m, nil
	}
	return m.dispatchToScreen(msg)
}

// dispatchToScreen routes msg to whichever screen is current, with none of
// handleKey's own global-binding or filter-state handling applied first --
// split out so both handleKey's ordinary path and its two filter-deferral
// cases above can reach a screen's own key handling without duplicating the
// six-way switch.
func (m Model) dispatchToScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.currentScreen() {
	case screenHome:
		return m.updateHome(msg)
	case screenDocument:
		return m.updateDocument(msg)
	case screenConfirm:
		return m.updateConfirm(msg)
	case screenValuePrompt:
		return m.updateValuePrompt(msg)
	case screenDetail:
		return m.updateDetail(msg)
	case screenSettings:
		return m.updateSettings(msg)
	}
	return m, nil
}

// handleBack applies the global back binding: dismiss a Detail overlay
// first (it is on top of everything else -- see deriveScreen), then decline
// a pending Confirm/ValuePrompt question (Session.Answer(false)) -- mirrors
// confirmation_sheet.dart's ConfirmationSheetHost answering false on every
// dismissal that is not the sheet's own Confirm/Send button (see that
// file's module comment), translated here to "the shell's own Back key is
// every dismissal this screen does not handle itself" since a terminal has
// no tap-outside/drag/system-back-gesture equivalent to unify -- otherwise
// pop Session's navigation stack if there is anywhere to pop to, otherwise
// return to Home. Answer(false), DismissDetail and CanGoBack/Back are all
// called directly, never through sessionCmd: none of the three perform any
// I/O -- Answer(false) only clears the pending question without sending
// anything (action.Answer's own contract), DismissDetail only clears a
// field already in hand, and Back only pops an already-fetched frame off
// Session's own stack (internal/nav.Nav.Back) rather than fetching anything
// -- so wrapping any of them in a tea.Cmd would add a goroutine hop for no
// reason. Compare Session.Refresh or Session.Activate on a
// render.FetchTarget, either of which does reach the network and so must go
// through sessionCmd.
func (m Model) handleBack() Model {
	if m.session.Detail() != nil {
		m.session.DismissDetail()
		return m
	}
	if m.session.Question() != nil {
		m.session.Answer(false)
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
// (home.go), Document's own view (document.go), Confirm's own view
// (confirm.go), ValuePrompt's own view (valueprompt.go), Detail's own view
// (detail.go) and Settings' own view (settings.go). Every screen constant
// screen.go declares now has a case here, so -- unlike while Detail was
// still a placeholder task 731 had yet to fill in -- this is a closed switch
// with a default-panics canary, the same convention render.RowTarget and
// session.SessionQuestion's own switches follow: a screen constant added
// later with a forgotten case here fails loudly the first time that screen
// is reached, instead of silently rendering nothing.
func (m Model) currentScreenView() string {
	switch m.currentScreen() {
	case screenHome:
		return m.home.View()
	case screenDocument:
		return m.document.View(m.session)
	case screenConfirm:
		return confirmView(m.confirmQuestion())
	case screenValuePrompt:
		return m.valuePrompt.View(m.valueQuestion())
	case screenDetail:
		return m.detail.View(m.detailView())
	case screenSettings:
		return m.settings.View()
	default:
		panic(fmt.Sprintf("tui: unreachable screen %v", m.currentScreen()))
	}
}

// confirmQuestion, valueQuestion and detailView re-assert Session.Question()/
// Session.Detail() to the concrete type or value deriveScreen already
// established when it picked screenConfirm/screenValuePrompt/screenDetail as
// current -- panicking on a mismatch: deriveScreen and this switch must
// always agree on what session.SessionQuestion's dynamic type (or
// Session.Detail's nilness) means, so disagreement is this package's own
// bug, not a state a caller can hit by pressing the wrong key. Mirrors the
// default-panics-as-canary convention every switch over a closed interface
// in this codebase follows (see internal/action/ask.go's AskOutcome doc
// comment) -- split into three small helpers, rather than inlined in
// currentScreenView, so each stays a one-line call at its use site.
func (m Model) confirmQuestion() session.ConfirmQuestion {
	q, ok := m.session.Question().(session.ConfirmQuestion)
	if !ok {
		panic(fmt.Sprintf("tui: screenConfirm current but Question() is %T", m.session.Question()))
	}
	return q
}

func (m Model) valueQuestion() session.ValueQuestion {
	q, ok := m.session.Question().(session.ValueQuestion)
	if !ok {
		panic(fmt.Sprintf("tui: screenValuePrompt current but Question() is %T", m.session.Question()))
	}
	return q
}

func (m Model) detailView() session.DetailView {
	d := m.session.Detail()
	if d == nil {
		panic("tui: screenDetail current but Detail() is nil")
	}
	return *d
}
