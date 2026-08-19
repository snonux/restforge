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
//
// # What this package deliberately does not do yet
//
// Beyond Home, every other screen still does nothing but name itself:
// Document does not render rows, Settings does not edit a backend, and so
// on. Each is a placeholder task 531/931/631/731 (see this project's task
// tracker) fills in. This package's job is the shell around them -- the
// Model, the navigation between screen states, the shared styles and the
// async pattern -- plus, now, the one screen (Home) that shell exists to
// show first.
package tui
