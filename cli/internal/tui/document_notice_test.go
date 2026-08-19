// Tests for the action-outcome/live-progress banners (document_notice.go,
// task 831). The rendering itself is pure -- noticeBannerView,
// watchingBannerView, outcomeBannerView and dismissibleNoticeShowing take a
// documentSource (or a session.SessionNotice) and return a string or bool --
// so these cases drive them through fakeDocumentSource rather than a full
// Session wired to a live watch, the same way document_test.go's own
// banner-less cases do. The Session-driven paths (the live tick loop, the
// spinner start/stop, the createTimer posting) live in live_cmd_test.go.
package tui

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/session"
)

// noticeView builds a documentModel and renders its notice banner against
// src -- the one-line setup every noticeBannerView test shares.
func noticeView(src fakeDocumentSource) string {
	return newDocumentModel().noticeBannerView(src)
}

// --- outcomeBannerView: one assertion per notice kind --------------------

func TestOutcomeBannerReportedIsSuccessStyled(t *testing.T) {
	notice := session.ActionOutcomeReported{
		Heading: "Brew", Message: "done", Body: "id: 7",
	}
	got := outcomeBannerView(notice)
	want := SuccessStyle.Render("done: id: 7")
	if got != want {
		t.Errorf("outcomeBannerView(Reported) = %q, want %q", got, want)
	}
	// And it must NOT read as an error or a neutral info banner -- the
	// beige/orange confusion commit 858df48 fixed (see InfoStyle's doc
	// comment): a finished success is its own colour, distinct from both.
	if got == ErrorStyle.Render("done: id: 7") {
		t.Error("a reported outcome rendered with ErrorStyle, want SuccessStyle")
	}
	if got == InfoStyle.Render("done: id: 7") {
		t.Error("a reported outcome rendered with InfoStyle, want SuccessStyle")
	}
}

func TestOutcomeBannerFailedIsErrorStyled(t *testing.T) {
	notice := session.ActionFailed{
		Heading: "Brew",
		Failure: &failure.Failure{Kind: failure.Conflict, Message: "already running"},
	}
	got := outcomeBannerView(notice)
	want := ErrorStyle.Render("Brew failed: already running")
	if got != want {
		t.Errorf("outcomeBannerView(Failed) = %q, want %q", got, want)
	}
	if got == SuccessStyle.Render("Brew failed: already running") {
		t.Error("a failed outcome rendered with SuccessStyle, want ErrorStyle")
	}
	if got == InfoStyle.Render("Brew failed: already running") {
		t.Error("a failed outcome rendered with InfoStyle, want ErrorStyle: a failure is neither success nor neutral")
	}
}

func TestOutcomeBannerRefusedIsErrorStyled(t *testing.T) {
	notice := session.ActionRefused{Heading: "Label", Reason: "Nothing was heard, so nothing was sent."}
	got := outcomeBannerView(notice)
	want := ErrorStyle.Render("Label: Nothing was heard, so nothing was sent.")
	if got != want {
		t.Errorf("outcomeBannerView(Refused) = %q, want %q", got, want)
	}
	if got == SuccessStyle.Render("Label: Nothing was heard, so nothing was sent.") {
		t.Error("a refused outcome rendered with SuccessStyle, want ErrorStyle")
	}
	if got == InfoStyle.Render("Label: Nothing was heard, so nothing was sent.") {
		t.Error("a refused outcome rendered with InfoStyle, want ErrorStyle: a refusal is neither success nor neutral")
	}
}

func TestOutcomeBannerGaveUpIsInfoStyled(t *testing.T) {
	notice := session.ActionGaveUp{Heading: "Brew"}
	got := outcomeBannerView(notice)
	want := InfoStyle.Render("Gave up waiting for \"Brew\" to finish")
	if got != want {
		t.Errorf("outcomeBannerView(GaveUp) = %q, want %q", got, want)
	}
	// Gave up is neither a failure nor a success -- it is "this app stopped
	// asking", not "the job failed" -- so it must not share either colour.
	if got == ErrorStyle.Render(`Gave up waiting for "Brew" to finish`) {
		t.Error("a gave-up outcome rendered with ErrorStyle, want InfoStyle")
	}
	if got == SuccessStyle.Render(`Gave up waiting for "Brew" to finish`) {
		t.Error("a gave-up outcome rendered with SuccessStyle, want InfoStyle")
	}
}

func TestOutcomeBannerWithdrawnIsInfoStyled(t *testing.T) {
	notice := session.ActionWithdrawn{Heading: "brew"}
	got := outcomeBannerView(notice)
	want := InfoStyle.Render("\"brew\" is no longer offered")
	if got != want {
		t.Errorf("outcomeBannerView(Withdrawn) = %q, want %q", got, want)
	}
	if got == ErrorStyle.Render("\"brew\" is no longer offered") {
		t.Error("a withdrawn outcome rendered with ErrorStyle, want InfoStyle: the server declining to offer an action is a real answer, not a failure")
	}
	if got == SuccessStyle.Render("\"brew\" is no longer offered") {
		t.Error("a withdrawn outcome rendered with SuccessStyle, want InfoStyle: withdrawn is neutral, not a success")
	}
}

func TestOutcomeBannerProgressIsInfoStyledAndReachable(t *testing.T) {
	// ActionProgress is routed to watchingBannerView in practice (see
	// outcomeBannerView's own doc comment), but the switch case exists to
	// stay exhaustive -- pin that it renders rather than panics, in the
	// colour watchingBannerView also uses, and that the colour is distinct
	// from both success and error so a running step never reads as either.
	notice := session.ActionProgress{Heading: "Brew", Step: "steeping"}
	got := outcomeBannerView(notice)
	if want := InfoStyle.Render("steeping"); got != want {
		t.Errorf("outcomeBannerView(Progress) = %q, want %q", got, want)
	}
	if got == ErrorStyle.Render("steeping") {
		t.Error("a progress step rendered with ErrorStyle, want InfoStyle: still running is not a failure")
	}
	if got == SuccessStyle.Render("steeping") {
		t.Error("a progress step rendered with SuccessStyle, want InfoStyle: still running is not a success")
	}
}

// --- noticeBannerView: the IsLive routing -------------------------------

func TestNoticeBannerEmptyWhenNotLiveAndNoNotice(t *testing.T) {
	if got := noticeView(fakeDocumentSource{state: nav.StateOK}); got != "" {
		t.Errorf("noticeBannerView with no notice = %q, want empty", got)
	}
}

func TestNoticeBannerRoutesLiveToWatchingBanner(t *testing.T) {
	// IsLive true: the banner is the watching one, even though the notice
	// itself is an ActionOutcomeReported (the moment before the first poll
	// lands, Session.handleSuccess has set that from the 202/running reply).
	got := noticeView(fakeDocumentSource{
		state:  nav.StateOK,
		isLive: true,
		notice: session.ActionOutcomeReported{Heading: "Brew", Message: "running"},
		doc:    twoRowDoc(),
	})
	if !contains(got, "running") {
		t.Errorf("watching banner = %q, want it to show the running message", got)
	}
}

func TestNoticeBannerRoutesNonLiveToOutcomeBanner(t *testing.T) {
	got := noticeView(fakeDocumentSource{
		state:  nav.StateOK,
		notice: session.ActionWithdrawn{Heading: "brew"},
		doc:    twoRowDoc(),
	})
	if want := InfoStyle.Render("\"brew\" is no longer offered"); got != want {
		t.Errorf("noticeBannerView(non-live Withdrawn) = %q, want %q", got, want)
	}
}

// --- dismissibleNoticeShowing ------------------------------------------

func TestDismissibleNoticeTrueForNonLiveNotice(t *testing.T) {
	d := newDocumentModel()
	if !d.dismissibleNoticeShowing(fakeDocumentSource{
		notice: session.ActionWithdrawn{Heading: "brew"},
	}) {
		t.Error("dismissibleNoticeShowing = false for a non-live notice, want true")
	}
}

func TestDismissibleNoticeFalseWhileLive(t *testing.T) {
	d := newDocumentModel()
	if d.dismissibleNoticeShowing(fakeDocumentSource{
		isLive: true,
		notice: session.ActionProgress{Step: "steeping"},
	}) {
		t.Error("dismissibleNoticeShowing = true while live, want false: \"still happening\" is never dismissible")
	}
}

func TestDismissibleNoticeFalseWithNoNotice(t *testing.T) {
	d := newDocumentModel()
	if d.dismissibleNoticeShowing(fakeDocumentSource{}) {
		t.Error("dismissibleNoticeShowing = true with no notice, want false")
	}
}

// --- hintLine advertises 'd' only when a dismissible banner is showing ---

func TestHintLineAdvertisesDismissForDismissibleNotice(t *testing.T) {
	d := newDocumentModel()
	hint := d.hintLine(fakeDocumentSource{
		doc:    twoRowDoc(),
		state:  nav.StateOK,
		notice: session.ActionWithdrawn{Heading: "brew"},
	})
	if !contains(hint, "dismiss") {
		t.Errorf("hintLine with a dismissible notice = %q, want it to advertise dismiss", hint)
	}
}

func TestHintLineAdvertisesDismissForFailureBanner(t *testing.T) {
	d := newDocumentModel()
	hint := d.hintLine(fakeDocumentSource{
		doc:     twoRowDoc(),
		state:   nav.StateError,
		failure: &failure.Failure{Kind: failure.Server, Message: "down"},
	})
	if !contains(hint, "dismiss") {
		t.Errorf("hintLine with a failure banner = %q, want it to advertise dismiss", hint)
	}
}

func TestHintLineOmitsDismissWhenNothingDismissible(t *testing.T) {
	d := newDocumentModel()
	hint := d.hintLine(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateOK})
	if contains(hint, "dismiss") {
		t.Errorf("hintLine with nothing dismissible = %q, want no dismiss hint", hint)
	}
}

func TestHintLineOmitsDismissWhileLive(t *testing.T) {
	d := newDocumentModel()
	hint := d.hintLine(fakeDocumentSource{
		doc:    twoRowDoc(),
		state:  nav.StateOK,
		isLive: true,
		notice: session.ActionProgress{Step: "steeping"},
	})
	if contains(hint, "dismiss") {
		t.Errorf("hintLine while live = %q, want no dismiss hint: the watching banner is not dismissible", hint)
	}
}

// --- View splices the notice banner in over the rows --------------------

func TestDocumentViewShowsNoticeBannerOverRows(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())

	view := d.View(fakeDocumentSource{
		doc:    twoRowDoc(),
		state:  nav.StateOK,
		notice: session.ActionWithdrawn{Heading: "brew"},
	})

	if !contains(view, "is no longer offered") {
		t.Errorf("View() = %q, want it to render the notice banner over the rows", view)
	}
	if !contains(view, "Root") {
		t.Errorf("View() = %q, want the document title still underneath the banner", view)
	}
}

func TestDocumentViewShowsWatchingBannerWhileLive(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())

	view := d.View(fakeDocumentSource{
		doc:    twoRowDoc(),
		state:  nav.StateOK,
		isLive: true,
		notice: session.ActionProgress{Step: "steeping"},
	})

	if !contains(view, "steeping") {
		t.Errorf("View() while live = %q, want it to show the progress step", view)
	}
}

func TestDocumentViewOmitsNoticeBannerWhenNone(t *testing.T) {
	d := newDocumentModel().resize(80, 24)
	d = d.syncRows(twoRowDoc())

	view := d.View(fakeDocumentSource{doc: twoRowDoc(), state: nav.StateOK})
	if contains(view, "no longer offered") || contains(view, "Still running") {
		t.Errorf("View() with no notice = %q, want no notice banner spliced in", view)
	}
}

// contains is the same tiny substring helper other test files in this
// package use -- kept local rather than shared across files for the same
// reason each existing one gives (a handful of lines, no other coupling).
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
