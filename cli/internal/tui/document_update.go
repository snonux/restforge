package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snonux/restforge/cli/internal/session"
)

// documentEnterBinding, documentDismissBinding and documentSaveQuickBinding
// are the Document screen's own key bindings, kept out of keys.go's shared
// keyMap per that file's own doc comment: a screen-specific key is that
// screen's own to add, not folded into the three bindings every screen
// shares. None carries a key.WithHelp -- see documentModel.hintLine
// (document.go) for where this screen's own hint text lives instead.
// documentDismissBinding clears whichever of the failure banner, the notice
// banner and the quick-save banner is currently dismissible -- all at once,
// if more than one is showing -- see dismissDocumentBanners.
var (
	documentEnterBinding     = key.NewBinding(key.WithKeys("enter"))
	documentDismissBinding   = key.NewBinding(key.WithKeys("d"))
	documentSaveQuickBinding = key.NewBinding(key.WithKeys("s"))
)

// updateDocument routes a key event on the Document screen: Enter activates
// the row under the cursor (activateDocumentSelection), 'd' dismisses
// whichever banner is showing (documentModel.dismissedFailure's own doc
// comment explains why this screen needs a dismiss key the Dart original
// does not), 's' saves the row under the cursor as a shortcut
// (saveSelectedQuick -- mirrors document_rows.dart's long-press affordance,
// translated to a key the same way home_update.go's deleteSelectedQuick
// translates home_screen.dart's remove-shortcut icon button), and
// everything else -- arrows, j/k/h/l, page up/down -- is forwarded to the
// row list unchanged, the same split updateHome makes for Home's two lists.
// Called from Model.handleKey's fallback once the three global bindings
// (quit/back/help) have all missed and the current screen is Document.
//
// Skips straight to forwarding, none of the above applied, while the row
// list's own filter input has focus (list.Filtering) -- 'd' and 's' must
// reach a filter query being typed rather than firing on every keystroke of
// a search, the same deferral updateHome makes for its own overrides.
func (m Model) updateDocument(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.currentFilterState() == list.Filtering {
		var cmd tea.Cmd
		m.document.rows, cmd = m.document.rows.Update(msg)
		return m, cmd
	}
	switch {
	case key.Matches(msg, documentDismissBinding) && m.hasDismissibleBanner():
		return m.dismissDocumentBanners(), nil
	case key.Matches(msg, documentEnterBinding):
		return m.activateDocumentSelection()
	case key.Matches(msg, documentSaveQuickBinding):
		return m.saveSelectedQuick()
	}
	var cmd tea.Cmd
	m.document.rows, cmd = m.document.rows.Update(msg)
	return m, cmd
}

// hasDismissibleBanner reports whether 'd' currently has anything to do --
// see documentDismissBinding's own doc comment.
func (m Model) hasDismissibleBanner() bool {
	return m.session.Failure() != nil || m.document.dismissibleNoticeShowing(m.session) || m.document.quickNotice != ""
}

// dismissDocumentBanners clears whichever of the failure banner
// (documentModel.dismissedFailure), the notice banner
// (Session.DismissNotice) and the quick-save banner (documentModel.
// quickNotice) is currently showing and dismissible -- all, if more than
// one is. Neither Session call performs any I/O -- recording a dismissed
// failure only sets a field already in hand, and DismissNotice only clears
// Session's own field the same way DismissDetail does (see Model.handleBack's
// own doc comment on why calls like that never go through sessionCmd) -- so
// this needs no tea.Cmd of its own.
func (m Model) dismissDocumentBanners() Model {
	if m.session.Failure() != nil {
		m.document.dismissedFailure = m.session.Failure()
	}
	if m.document.dismissibleNoticeShowing(m.session) {
		m.session.DismissNotice()
	}
	m.document.quickNotice = ""
	m.document.quickNoticeFailed = false
	return m
}

// saveSelectedQuick saves the row the cursor is currently on as a shortcut,
// via Session.SaveQuick (saveQuickCmd, document_cmd.go) -- a no-op with
// nothing selected (an empty document). Whether the row is actually
// saveable (an action or a link, per SaveQuick's own doc comment) is not
// checked here: SaveQuick already refuses a property/embedded-entity row
// with QuickSaveNotSaveable rather than an error, and applyQuickSaved
// reports that refusal the same way it reports success, so this key does
// the same thing on every row and lets Session decide what happened.
func (m Model) saveSelectedQuick() (tea.Model, tea.Cmd) {
	item, ok := m.document.rows.SelectedItem().(documentRowItem)
	if !ok {
		return m, nil
	}
	return m, saveQuickCmd(m.session, item.row)
}

// applyQuickSaved folds a documentQuickSavedMsg into the Document screen's
// own quickNotice banner -- an error report for a genuine failure
// (SaveQuick's own storage write erroring) or a plain-English restatement of
// whichever session.QuickSaveOutcome it returned, mirroring
// document_rows.dart's own SnackBar text for each case (_saveQuickShortcut).
// On QuickSaveSaved, also kicks off homeInitCmd in the background -- Home's
// own shortcuts list (homeModel.shortcuts) is loaded once at startup and
// otherwise only refreshed by applyQuickDeleted/applySettingsSaved, so
// without this a shortcut saved from here would be invisible on Home until
// something else happened to trigger a reload; the reload runs and lands
// regardless of which screen is current by the time it completes (it only
// ever touches m.home), so it is safe to start immediately rather than
// waiting for the user to actually navigate back to Home.
func (m Model) applyQuickSaved(msg documentQuickSavedMsg) (tea.Model, tea.Cmd) {
	m.document.quickNoticeFailed = true
	switch {
	case msg.err != nil:
		m.document.quickNotice = "could not save shortcut: " + msg.err.Error()
	case msg.outcome == session.QuickSaveSaved:
		m.document.quickNotice = fmt.Sprintf("saved %q as a shortcut", msg.label)
		m.document.quickNoticeFailed = false
		return m, homeInitCmd()
	case msg.outcome == session.QuickSaveFull:
		m.document.quickNotice = "shortcut list is full"
	case msg.outcome == session.QuickSaveNotSaveable:
		m.document.quickNotice = "this row cannot be saved as a shortcut"
	}
	return m, nil
}

// activateDocumentSelection activates the row the cursor is currently on:
// Session.Activate, wrapped in sessionCmd (cmd.go) since it may reach the
// network (render.FetchTarget) -- exactly the async pattern the tui shell
// task established for every Session method that performs I/O. The
// resulting screen transition (push Document again for a FetchTarget/
// EmbeddedTarget, or hand off to Confirm/ValuePrompt for an ActionTarget --
// built in task 631) is not decided here: it falls out of deriveScreen once
// Session's own state has changed, the same way every other sessionCmd
// caller in this package leaves that decision to Update. Nothing happens
// with nothing selected (an empty document).
func (m Model) activateDocumentSelection() (tea.Model, tea.Cmd) {
	item, ok := m.document.rows.SelectedItem().(documentRowItem)
	if !ok {
		return m, nil
	}
	target := item.row.Target
	return m, sessionCmd(func() { m.session.Activate(target) })
}
