package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces a colour profile for the whole test binary. In a bare
// `go test` environment Lip Gloss detects no colour support and strips
// every style to plain text, which would make SuccessStyle, ErrorStyle and
// InfoStyle render identically -- and the notice-banner tests (task 831)
// exist to pin that they do not (see commit 858df48 for the exact
// "neutral reads as warning/success" confusion that is the whole point of
// the three being distinct). Forcing TrueColor emits real colour codes so
// those assertions can distinguish them.
//
// Substring-based view tests elsewhere in this package are unaffected:
// colour codes wrap the text they already look for with strings.Contains,
// so a coloured "Settings" still contains "Settings".
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
