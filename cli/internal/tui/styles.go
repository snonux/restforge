package tui

import "github.com/charmbracelet/lipgloss"

// Colour palette, kept as unexported values only [Styles] below is built
// from -- a screen reaches for one of the styles, never one of these
// colours directly, so a palette change stays a one-file edit. Each colour
// is a lipgloss.AdaptiveColor (a light and a dark variant) rather than a
// single hex string, since a terminal's own background is unknown ahead of
// time and Lip Gloss already picks the right half for us; there is no
// equivalent choice to make in flutter/lib/main.dart, whose light/dark
// ThemeData pair this mirrors, because Flutter's ColorScheme.fromSeed
// derives both itself.
var (
	// colorPrimary echoes flutter/lib/main.dart's
	// ColorScheme.fromSeed(seedColor: Colors.deepOrange) -- the one colour
	// both ports share on purpose, so a screenshot of either app reads as
	// the same product.
	colorPrimary = lipgloss.AdaptiveColor{Light: "#B34700", Dark: "#FF8A50"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#9B9B9B"}
	colorError   = lipgloss.AdaptiveColor{Light: "#AF0000", Dark: "#FF6E6E"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#00875A", Dark: "#5FD787"}
	colorBorder  = lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#444444"}
)

// Styles are the palette every screen in this package renders through,
// kept in this one file so the TUI reads as one visual system rather than
// N independently-styled screens -- the same reasoning
// flutter/lib/main.dart's single RestForgeApp.build gives for choosing its
// ThemeData/darkTheme once, at the root, rather than per screen.
//
// A style here says what a *kind* of thing looks like (a title, a piece of
// muted help text, an error banner), never what one specific screen's copy
// of that thing looks like -- a screen composes these, it does not extend
// or override them, so a later palette change (a new brand colour, a
// higher-contrast terminal mode) touches only this file.
var (
	// TitleStyle is a screen's own heading -- "Home", "Document", the
	// current backend's name, and so on.
	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)

	// SubtitleStyle is secondary text directly under a TitleStyle heading:
	// Home's "Backends"/"Shortcuts" section labels (home.go) and Settings'
	// own "Backends" label (settings_items.go) today, a document's subtitle
	// once render.RenderedDocument has one to show.
	SubtitleStyle = lipgloss.NewStyle().Foreground(colorMuted)

	// HelpStyle is the key-binding hint line every screen shows, rendered
	// through bubbles/help -- see keys.go.
	HelpStyle = lipgloss.NewStyle().Foreground(colorMuted)

	// MutedStyle is de-emphasised body text: a sublabel, a disabled row, a
	// value not worth drawing the eye to.
	MutedStyle = lipgloss.NewStyle().Foreground(colorMuted)

	// ErrorStyle marks a transport failure -- Session.Failure -- or an
	// action outcome that must not be mistaken for success. Bold on
	// purpose: a failed request is not an answer (docs/DESIGN.md), and the
	// banner reporting it should not be easy to skim past.
	ErrorStyle = lipgloss.NewStyle().Bold(true).Foreground(colorError)

	// SuccessStyle marks an action outcome that plainly succeeded. Kept
	// distinct from ErrorStyle and from whatever a later task chooses for
	// "in progress" -- see commit 858df48 (cli history) for the exact
	// beige/orange confusion between neutral and success/warning states
	// that a live-progress banner (a later task) must not repeat.
	SuccessStyle = lipgloss.NewStyle().Foreground(colorSuccess)

	// SelectedItemStyle marks the row a bubbles/list cursor is currently
	// on. Declared here, ahead of the Home/Document screens that are the
	// first to need it, so both reach for the same style rather than each
	// picking its own.
	SelectedItemStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)

	// NormalItemStyle is every row a bubbles/list cursor is not currently
	// on -- the counterpart to SelectedItemStyle.
	NormalItemStyle = lipgloss.NewStyle()

	// OverlayBorderStyle frames a modal laid over the Document screen --
	// Confirm, ValuePrompt, Detail (see screen.go) -- so all three read as
	// the same kind of thing appearing on top of the document, the way
	// document_screen.dart's overlays share one visual language.
	OverlayBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).
				Padding(0, 1)

	// AppStyle is the outermost padding wrapping the whole rendered frame,
	// applied once by Model.View so no screen has to think about the
	// terminal's own edge.
	AppStyle = lipgloss.NewStyle().Padding(1, 2)
)
