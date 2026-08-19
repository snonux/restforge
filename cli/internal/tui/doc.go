// Package tui is the interactive Bubble Tea front end for restforge -- the
// terminal sibling of the Flutter app's UI, driven entirely by
// internal/session.
//
// # Files
//
//   - run.go builds the production internal/session.Session (a
//     *httpclient.Client composing internal/nav, internal/action and
//     internal/live, exactly as internal/cli's one-shot commands build one --
//     see act.go's runAct) and hands it to tea.NewProgram. Run is the only
//     exported entry point; internal/cli's no-subcommand path calls it.
//   - model.go is the root Bubble Tea Model: Init/Update/View, the global
//     key bindings, and deriveScreen, which decides which screen is on top
//     right now from Session's own overlay state. See its doc comment for
//     the async I/O pattern every later screen task (Home, Document,
//     Confirm, ValuePrompt, Detail, Settings) reuses.
//   - screen.go is the closed set of top-level views this shell switches
//     between. Every one of them beyond a bare placeholder is a later
//     task's job -- see each screen constant's doc comment for which task.
//   - keys.go is the global key map (quit, back, help) shared by every
//     screen; a screen-specific key (yes/no on Confirm, submit on
//     ValuePrompt, and so on) is that screen's own task to add.
//   - styles.go is the one Lip Gloss palette every screen renders through,
//     the same spirit as flutter/lib/main.dart's ThemeData: a screen reaches
//     for a style declared here rather than inlining its own
//     lipgloss.NewStyle(), so the whole TUI reads as one visual system.
//   - home.go, home_items.go, home_update.go and home_cmd.go are the Home
//     screen: the configured-backend and saved-quick-shortcut picker, two
//     bubbles/list.Models reading internal/config and internal/quick
//     directly when Home mounts (see home_cmd.go's homeInitCmd), never
//     through Session -- mirrors nav_service.dart's module comment that the
//     backend picker is out of NavService's scope. Picking a backend opens
//     it (Session.OpenBackend); picking a shortcut runs it
//     (Session.RunQuick); either switches the shell to the Document screen.
//     The 's' binding (homeSettingsBinding, home_update.go) opens Settings.
//   - document.go, document_items.go, document_update.go and
//     document_banner.go are the Document screen: Session.Document's rows,
//     with State/Failure overlaid on top.
//   - settings.go, settings_items.go, settings_form.go, settings_update.go
//     and settings_cmd.go are the Settings screen: the backend editor,
//     reached from Home's 's' binding or its empty state. A working copy of
//     the backend list (seeded from Home's own list, never re-read from
//     internal/config) is edited through five bubbles/textinput.Models --
//     one backend at a time, in its own mode -- validated through
//     internal/backend.Normalise/Validate exactly like every other caller,
//     and only reaches internal/config.SaveBackends once the user explicitly
//     saves; see settings.go's own package-level doc comment for the fuller
//     reasoning, including how this screen's two-mode shape differs from
//     flutter/lib/screens/settings_screen.dart's single all-cards-at-once
//     view and why.
//
// # What this package deliberately does not do yet
//
// Confirm and ValuePrompt (session.SessionQuestion's two concrete answers)
// and Detail (Session.Detail()) still do nothing but name themselves -- each
// is a placeholder task 631/731 (see this project's task tracker) fills in.
// This package's job is the shell around every screen -- the Model, the
// navigation between screen states, the shared styles and the async pattern
// -- plus, now, Home, Document and Settings, the three screens that shell
// exists to show.
package tui
