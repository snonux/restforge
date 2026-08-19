// Part of the Document screen (task 531): the failure-banner rendering
// (failureBannerView, fullScreenFailureView, failureBannerText), split out
// of document.go the same way document_banners.dart's part file is split
// out of document_screen.dart. The action-outcome/live-progress banners for
// Session.Notice are a later task (831) and deliberately do not live here.
package tui

import (
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/nav"
)

// failureBannerView renders the failure banner laid over the row list --
// mirrors _FailureBanner in document_banners.dart, plus the dismiss hint
// this port adds (see documentModel.dismissedFailure's own doc comment).
// Returns "" once src.Failure() is nil or has already been dismissed, so a
// caller (View, hintLine) can splice it in without an extra nil check of
// its own.
func (d documentModel) failureBannerView(src documentSource) string {
	f := src.Failure()
	if f == nil || f == d.dismissedFailure {
		return ""
	}
	return ErrorStyle.Render(failureBannerText(f, src.State() == nav.StateUnreachable))
}

// fullScreenFailureView is what emptyView shows for a failure with no
// document underneath it to protect -- mirrors _FullScreenFailure in
// document_banners.dart. Distinguished from failureBannerView only by
// taking the whole screen rather than sitting on top of rows that do not
// exist yet -- and by never being dismissible, since there is nothing
// underneath it to fall back to either way.
func fullScreenFailureView(f *failure.Failure, unreachable bool) string {
	heading := "The request failed"
	if unreachable {
		heading = "Could not reach the server"
	}
	body := ErrorStyle.Render(heading)
	if f != nil {
		body += "\n" + MutedStyle.Render(f.Message)
	}
	return TitleStyle.Render("Document") + "\n\n" + body
}

// failureBannerText words the failure banner's one line -- mirrors
// _FailureBanner's own unreachable/not-unreachable wording in
// document_banners.dart. docs/DESIGN.md's "A failed request is not an
// answer": "I could not ask" (unreachable) and "the answer was no" (every
// other failure kind) are different facts, and the wording keeps them from
// reading the same.
func failureBannerText(f *failure.Failure, unreachable bool) string {
	if unreachable {
		return "Unreachable: " + f.Message
	}
	return "Request failed: " + f.Message
}
