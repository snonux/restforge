package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"

	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/session"
)

// documentSource is the subset of *internal/session.Session the Document
// screen reads: Document, State and Failure -- the three accessors
// nav_service.dart's module comment requires a caller to overlay on top of
// each other rather than let one replace another (State/Failure lay over
// Document, never instead of it) -- plus Notice and IsLive (task 831),
// which document_notice.go overlays the same way, one layer further out.
// Its own interface, mirroring screenSource in derive.go, so a test can
// drive documentModel with a plain fake instead of a full Session wired to
// real nav/action/live instances.
type documentSource interface {
	Document() *render.RenderedDocument
	State() nav.DocumentState
	Failure() *failure.Failure
	Notice() session.SessionNotice
	IsLive() bool
}

// *session.Session satisfies documentSource structurally; asserted here so
// a future signature change to any of the three methods is caught at
// compile time, rather than surfacing only when Model.currentScreenView
// calls documentModel.View.
var _ documentSource = (*session.Session)(nil)

// documentModel is the Document screen's own state: the row list plus
// enough bookkeeping to know when that list actually needs rebuilding.
// Mirrors homeModel's role for the Home screen (home.go) -- see that file's
// own doc comment for why a screen keeps its own bubbles/list.Model rather
// than deriving one fresh on every View call.
type documentModel struct {
	rows list.Model

	// fingerprint is documentFingerprint's output for whatever
	// render.RenderedDocument rows was last built from -- see syncRows.
	fingerprint string

	// dismissedFailure is the *failure.Failure the failure banner was last
	// dismissed for, compared by identity: nav.Nav's own fetch failure path
	// allocates a fresh *failure.Failure on every failed fetch, so a stale
	// dismissal can never hide a new failure -- see failureBannerView.
	// Unlike document_banners.dart's _FailureBanner, which is never
	// dismissed on its own (a phone has pull-to-refresh to get rid of it),
	// a terminal has no equivalent gesture, so this screen adds one key
	// (see documentDismissBinding, document_update.go) the Dart original
	// does not need.
	dismissedFailure *failure.Failure

	// spinner animates while Session.IsLive is true -- see
	// document_notice.go's watchingBannerView, which is the only place this
	// is rendered, and model.go's startSpinnerCmd/handleSpinnerTick for how
	// its tick loop is driven. Kept on documentModel rather than Model
	// itself since it belongs to the Document screen's own rendering the
	// same way d.rows does, even though what drives it (Session.IsLive) is
	// read at the Model.Update level.
	spinner spinner.Model
}

// newDocumentModel builds the row list on documentDelegate (document_items.go),
// at zero size -- Model.Update's tea.WindowSizeMsg case calls resize once the
// real terminal size is known, the same lazy-sizing newHomeModel uses. The
// list's own chrome is turned off for the same reasons newHomeModel's own
// doc comment gives: this screen renders its own title and hint line, and
// filtering is not worth the "esc" key it binds by default colliding with
// the shell's global Back binding (keys.go).
func newDocumentModel() documentModel {
	l := list.New(nil, documentDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return documentModel{rows: l, spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

// resize gives the row list its share of the available body height, after
// setting aside room for the title, a possible loading/failure line above
// it and the hint line below it -- mirrors homeModel.resize's own
// reasoning, generous rather than exact for the same stated reason.
func (d documentModel) resize(width, height int) documentModel {
	const chrome = 8
	avail := height - chrome
	if avail < 3 {
		avail = 3
	}
	d.rows.SetSize(width, avail)
	return d
}

// syncRows rebuilds the row list from doc, but only when doc actually
// differs from whatever it was last built from -- see documentFingerprint.
// Session.Document rebuilds a brand new *render.RenderedDocument on every
// call (nav.Nav.Document's own doc comment), so pointer identity cannot be
// used to detect "nothing changed"; a value fingerprint is the cheap
// substitute. Skipping the rebuild when nothing changed is what lets the
// list's own cursor position and scroll offset survive a redraw that has
// nothing to do with navigation (toggling help, a resize, an unrelated
// key) -- rebuilding unconditionally would reset the cursor to the top on
// every single keypress. Called from Model.Update at every point Session's
// own state may just have changed -- see model.go.
func (d documentModel) syncRows(doc *render.RenderedDocument) documentModel {
	fp := documentFingerprint(doc)
	if fp == d.fingerprint {
		return d
	}
	d.fingerprint = fp
	d.dismissedFailure = nil

	var items []list.Item
	if doc != nil {
		items = make([]list.Item, len(doc.Rows))
		for i, row := range doc.Rows {
			items[i] = documentRowItem{row: row}
		}
	}
	d.rows.SetItems(items)
	d.rows.Select(0)
	return d
}

// documentFingerprint is a cheap value fingerprint of doc's title and rows
// -- see syncRows. Two different documents that happen to render
// byte-for-byte identical rows are indistinguishable by this fingerprint (a
// document with no title and no rows is one example), which only means
// syncRows keeps the previous cursor position rather than resetting it in
// that one coincidental case -- a cosmetic near-miss, not a correctness
// problem, since the rows actually on screen are still exactly right either
// way.
func documentFingerprint(doc *render.RenderedDocument) string {
	if doc == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(doc.Title)
	for _, row := range doc.Rows {
		b.WriteByte(0)
		b.WriteString(strconv.Itoa(int(row.Kind)))
		b.WriteByte(0)
		b.WriteString(row.Label)
		b.WriteByte(0)
		b.WriteString(row.Sublabel)
	}
	return b.String()
}

// View renders the Document screen: the document's title, a loading line
// or failure banner laid over the rows without replacing them once
// something has been fetched, the row list itself (or a one-line note when
// the document has no rows at all), and a hint line -- mirrors
// _DocumentBody in document_screen.dart. Before anything has ever been
// fetched (src.Document() nil), emptyView takes over instead -- see its own
// doc comment for why that case gets a dedicated full-screen treatment
// rather than an empty list pretending to be one.
func (d documentModel) View(src documentSource) string {
	doc := src.Document()
	if doc == nil {
		return d.emptyView(src)
	}

	var b strings.Builder
	b.WriteString(TitleStyle.Render(doc.Title) + "\n\n")
	if src.State() == nav.StateLoading {
		b.WriteString(MutedStyle.Render("Loading...") + "\n")
	}
	if banner := d.failureBannerView(src); banner != "" {
		b.WriteString(banner + "\n")
	}
	if banner := d.noticeBannerView(src); banner != "" {
		b.WriteString(banner + "\n")
	}
	if len(doc.Rows) == 0 {
		b.WriteString(MutedStyle.Render("This document has nothing to show.") + "\n")
	} else {
		b.WriteString(d.rows.View())
	}
	b.WriteString("\n" + MutedStyle.Render(d.hintLine(src)))
	return b.String()
}

// hintLine is the Document screen's own short key hint -- keys.go's own
// doc comment reserves the shell's global three (quit/back/help) for
// bindings every screen shares, so Enter and the dismiss key are composed
// here instead, the same split home.go's hintLine makes for Tab. The same
// 'd' binding dismisses either banner (or both at once, if both are
// showing) -- see documentDismissBinding's own doc comment (document_update.go).
func (d documentModel) hintLine(src documentSource) string {
	if d.failureBannerView(src) != "" || d.dismissibleNoticeShowing(src) {
		return "↑/↓ move · enter select · d dismiss"
	}
	return "↑/↓ move · enter select"
}

// emptyView is what the Document screen shows before anything has ever
// been fetched, or when the very first fetch failed with nothing to fall
// back on -- mirrors _EmptyBody in document_screen.dart. There is no last
// good document to protect here, so Loading and a failure each get a
// plain, honest full-screen treatment instead of the row-list-plus-overlay
// View uses once something has landed.
func (d documentModel) emptyView(src documentSource) string {
	switch src.State() {
	case nav.StateLoading:
		return TitleStyle.Render("Document") + "\n" + MutedStyle.Render("Loading...")
	case nav.StateError, nav.StateUnreachable:
		return fullScreenFailureView(src.Failure(), src.State() == nav.StateUnreachable)
	default:
		// Reachable only in the moment before the first fetch has even
		// started -- not a state a person should ever sit on for long, but
		// rendering nothing at all would look indistinguishable from a
		// bug. Mirrors _EmptyBody's own DocumentState.ok case.
		return TitleStyle.Render("Document") + "\n" + MutedStyle.Render("Nothing to show yet.")
	}
}
