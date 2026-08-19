// Part of the Document screen (task 831): the action-outcome/live-progress
// banner rendering for Session.Notice -- split out of document.go the same
// way document_banner.go's failure banner is its own file, and deliberately
// its own file rather than folded into document_banner.go, which flags this
// as a separate, later task in its own doc comment. Mirrors
// _NoticeBanner/_watchingBanner/_outcomeBanner in document_banners.dart.
package tui

import (
	"fmt"

	"github.com/snonux/restforge/cli/internal/session"
)

// noticeBannerView renders whatever Session.Notice currently holds, laid
// over the row list below the failure banner -- mirrors _NoticeBanner.build.
// IsLive alone decides which of the two banners below applies: it is true
// for exactly "progress" and "no news" (watchingBannerView) and false for
// exactly every other case (outcomeBannerView) -- mirrors live_service.dart's
// own contract that OnGiveUp and OnDone each stop the watch before
// reporting (internal/live's package comment), so the two never overlap.
// Returns "" once there is nothing to show, so View can splice this in
// unconditionally the same way it does failureBannerView.
func (d documentModel) noticeBannerView(src documentSource) string {
	if src.IsLive() {
		return d.watchingBannerView(src.Notice())
	}
	notice := src.Notice()
	if notice == nil {
		return ""
	}
	return outcomeBannerView(notice)
}

// dismissibleNoticeShowing reports whether noticeBannerView is currently
// showing something documentDismissBinding can clear -- watchingBannerView
// never is (see its own doc comment on why "still happening" is never
// dismissible), so this is exactly "there is a notice, and it is not the
// live one". Used by hintLine (document.go) and updateDocument
// (document_update.go) to decide whether 'd' does anything.
func (d documentModel) dismissibleNoticeShowing(src documentSource) bool {
	return !src.IsLive() && src.Notice() != nil
}

// watchingBannerView is "progress" and "no news", covered by one banner on
// purpose -- see the module comment. An ActionProgress notice carries the
// step to show; the moment before the first poll lands, Notice is still the
// ActionOutcomeReported Session.handleSuccess set from the action's own
// 202/running reply, so that message is shown instead of a placeholder.
// Info-coloured (never success or error -- see InfoStyle's own doc comment)
// and never dismissible: dismissing "something is still happening" would be
// a lie, since the job keeps running underneath regardless of whether this
// banner is on screen -- mirrors _watchingBanner's own doc comment.
func (d documentModel) watchingBannerView(notice session.SessionNotice) string {
	text := "Still running"
	switch n := notice.(type) {
	case session.ActionProgress:
		text = n.Step
	case session.ActionOutcomeReported:
		text = n.Message
	}
	return InfoStyle.Render(d.spinner.View() + " " + text)
}

// outcomeBannerView is what is left once IsLive is false -- mirrors
// _outcomeBanner. Every case here is dismissible via Session.DismissNotice
// (documentDismissBinding, document_update.go) -- unlike
// watchingBannerView, each of these is a finished fact, not something
// still changing underneath the banner. Colouring follows commit 858df48's
// rule (cli history, ported from the same-numbered Flutter fix): "done" is
// success-coloured, the two real error cases (ActionFailed, ActionRefused)
// are error-coloured, and everything else -- gave up, withdrawn, and the
// ActionProgress case that is unreachable in practice, see below -- is
// info-coloured, never the same as either.
func outcomeBannerView(notice session.SessionNotice) string {
	switch n := notice.(type) {
	case session.ActionOutcomeReported:
		return SuccessStyle.Render(fmt.Sprintf("%s: %s", n.Message, n.Body))
	case session.ActionGaveUp:
		return InfoStyle.Render(fmt.Sprintf("Gave up waiting for %q to finish", n.Heading))
	case session.ActionFailed:
		return ErrorStyle.Render(fmt.Sprintf("%s failed: %s", n.Heading, n.Failure.Message))
	case session.ActionRefused:
		return ErrorStyle.Render(fmt.Sprintf("%s: %s", n.Heading, n.Reason))
	case session.ActionWithdrawn:
		return InfoStyle.Render(fmt.Sprintf("%q is no longer offered", n.Heading))
	case session.ActionProgress:
		// Unreachable in practice: Session only ever holds an ActionProgress
		// notice while IsLive is true, and noticeBannerView routes that case
		// to watchingBannerView instead -- see that method's own doc
		// comment. Handled anyway so this switch stays exhaustive against
		// SessionNotice gaining a case later without silently mis-rendering
		// it -- the default-panics-as-canary convention every switch over a
		// closed interface in this codebase follows (see
		// render.RowTarget's own doc comment).
		return InfoStyle.Render(n.Step)
	default:
		panic(fmt.Sprintf("tui: unreachable SessionNotice type %T", notice))
	}
}
