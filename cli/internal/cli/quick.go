package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/quick"
	"github.com/snonux/restforge/cli/internal/session"
)

// newQuickCmd builds the `restforge quick` parent and its three one-shot
// subcommands for scripting saved shortcuts without opening the TUI: list,
// remove and run. There is intentionally no `quick add`/`quick save` here:
// saving a shortcut only makes sense from something already on screen,
// which is a TUI-only affordance in both the Pebble and Flutter ports (see
// internal/session/doc.go), and a CLI equivalent would have nothing to save
// from. Each subcommand loads through internal/quick (which loads through
// internal/config); run drives internal/session.RunQuick and renders the
// result through the same output helpers restforge get and restforge act use
// (PrintDocumentText/PrintJSON in output.go), rather than duplicating that
// rendering.
func newQuickCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quick",
		Short: "manage saved shortcuts without opening the TUI",
		Long: "Manage the saved shortcuts stored in the config file -- list\n" +
			"them, remove one, or run one -- for scripting, without opening the\n" +
			"TUI. There is no `quick add`: saving a shortcut only makes sense\n" +
			"from a document already on screen, which is a TUI-only affordance.",
	}
	cmd.AddCommand(newQuickListCmd(g))
	cmd.AddCommand(newQuickRemoveCmd(g))
	cmd.AddCommand(newQuickRunCmd(g))
	return cmd
}

// --- list ----------------------------------------------------------------

// newQuickListCmd builds `restforge quick list`: print the saved shortcuts
// as a table (text, the default) or a JSON array (--output json). The
// columns are label, kind, backend and target -- the target is the href for
// a document shortcut, or the action name and the document it lives on for
// an action shortcut.
func newQuickListCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "print saved shortcuts as a table or JSON array",
		Long: "Print the saved shortcuts. The default text output is a\n" +
			"tab-separated table with one row per shortcut and a header line:\n" +
			"LABEL, KIND, BACKEND and TARGET. --output json prints the same\n" +
			"information as a JSON array (with holder/name for action\n" +
			"shortcuts, href for document shortcuts).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickList(cmd.OutOrStdout(), g)
		},
	}
	return cmd
}

// runQuickList is the list command's body, split out of the RunE closure so
// it reads as straight-line logic and a test can call it with a buffer.
func runQuickList(out io.Writer, g *globalFlags) error {
	items, err := quick.Load()
	if err != nil {
		return fmt.Errorf("quick list: %w", err)
	}
	if g.output == "json" {
		return printQuickJSON(out, items)
	}
	return printQuickText(out, items)
}

// printQuickText writes a header line followed by one tab-separated row per
// shortcut, mirroring backends list's shape so a caller can pipe either
// through cut/awk the same way.
func printQuickText(out io.Writer, items []quick.QuickItem) error {
	if _, err := fmt.Fprintln(out, "LABEL\tKIND\tBACKEND\tTARGET"); err != nil {
		return err
	}
	for _, item := range items {
		row := fmt.Sprintf("%s\t%s\t%s\t%s",
			item.Label, item.Kind, item.BackendName, quickTarget(item))
		if _, err := fmt.Fprintln(out, row); err != nil {
			return err
		}
	}
	return nil
}

// printQuickJSON writes shortcuts as a pretty-printed JSON array, with the
// kind-specific target split into holder/name (action) or href (document)
// so a caller piping into jq can branch on kind without re-parsing a string.
func printQuickJSON(out io.Writer, items []quick.QuickItem) error {
	rows := make([]quickItemJSON, len(items))
	for i, item := range items {
		rows[i] = newQuickItemJSON(item)
	}
	return PrintJSON(out, rows)
}

// quickItemJSON is the JSON shape of one row of `quick list --output json`.
// Href is set for document shortcuts; Holder and Name for action shortcuts.
type quickItemJSON struct {
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Backend string `json:"backend"`
	Href    string `json:"href,omitempty"`
	Holder  string `json:"holder,omitempty"`
	Name    string `json:"name,omitempty"`
}

// newQuickItemJSON builds the JSON row for item, carrying the kind-specific
// target fields so a jq caller can branch on kind without parsing a string.
func newQuickItemJSON(item quick.QuickItem) quickItemJSON {
	row := quickItemJSON{Label: item.Label, Kind: item.Kind.String(), Backend: item.BackendName}
	if item.Kind == quick.KindAction {
		row.Holder, row.Name = item.Holder, item.Name
	} else {
		row.Href = item.Href
	}
	return row
}

// quickTarget is the text-form target for one shortcut: the href for a
// document, or "name on holder" for an action. Presentation only -- nothing
// here builds a URL or interprets a rel (../../AGENTS.md, "Never build a
// URL").
func quickTarget(item quick.QuickItem) string {
	if item.Kind == quick.KindAction {
		return fmt.Sprintf("%s on %s", item.Name, item.Holder)
	}
	return item.Href
}

// --- remove --------------------------------------------------------------

// newQuickRemoveCmd builds `restforge quick remove LABEL-OR-INDEX`: drop a
// shortcut, addressed either by its label or by its 0-based index in
// `quick list`. A label that matches nothing (or more than one) is a
// config-state failure (exit 1); a missing NAME-style argument is a usage
// error (exit 2).
func newQuickRemoveCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove LABEL-OR-INDEX",
		Short: "remove a saved shortcut by label or index",
		Long: "Remove a saved shortcut from the config file. LABEL-OR-INDEX\n" +
			"is either the shortcut's label (an exact, unique match) or its\n" +
			"0-based index in `quick list`.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErrorf("remove takes exactly one LABEL-OR-INDEX argument, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickRemove(cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

// runQuickRemove is the remove command's body, split out of the RunE closure
// so it reads as straight-line logic and a test can call it.
func runQuickRemove(out io.Writer, arg string) error {
	items, err := quick.Load()
	if err != nil {
		return fmt.Errorf("quick remove: %w", err)
	}
	idx, err := resolveQuickItem(items, arg)
	if err != nil {
		return err
	}
	removed := items[idx]
	ok, err := quick.Remove(idx)
	if err != nil {
		return fmt.Errorf("quick remove: %w", err)
	}
	if !ok {
		// resolveQuickItem already bounded idx to the list, so a false here
		// means the list changed between the load above and quick.Remove's
		// own load -- treat it as not-found rather than a silent no-op.
		return fmt.Errorf("quick remove: no shortcut at index %d", idx)
	}
	_, _ = fmt.Fprintf(out, "removed shortcut %q\n", removed.Label)
	return nil
}

// --- run -----------------------------------------------------------------

// newQuickRunCmd builds `restforge quick run LABEL-OR-INDEX`: follow a saved
// shortcut through internal/session.RunQuick and render whatever document,
// question or notice results, using the same output helpers get and act use.
// A shortcut whose backend has since been removed produces a clear non-zero
// exit, never a silent no-op (see QuickRunBackendMissing).
func newQuickRunCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run LABEL-OR-INDEX",
		Short: "run a saved shortcut and print the result",
		Long: "Run a saved shortcut: adopt the backend it points at, fetch the\n" +
			"saved address (a document shortcut) or look the saved action up by\n" +
			"name on its holder (an action shortcut), and print whatever\n" +
			"results -- a document, a question an unsafe action asks, or a\n" +
			"notice. LABEL-OR-INDEX is the shortcut's label or its 0-based index\n" +
			"in `quick list`. A shortcut whose backend has been removed exits\n" +
			"non-zero with a clear message.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErrorf("run takes exactly one LABEL-OR-INDEX argument, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuickRun(cmd.OutOrStdout(), cmd.ErrOrStderr(), g, args[0])
		},
	}
	return cmd
}

// runQuickRun is the run command's body, split out of the RunE closure so it
// reads as straight-line logic and a test can call it with buffers.
func runQuickRun(out, errw io.Writer, g *globalFlags, arg string) error {
	items, err := quick.Load()
	if err != nil {
		return fmt.Errorf("quick run: %w", err)
	}
	idx, err := resolveQuickItem(items, arg)
	if err != nil {
		return err
	}
	item := items[idx]

	sess := newQuickSession()
	outcome, err := sess.RunQuick(item)
	if err != nil {
		return fmt.Errorf("quick run: %w", err)
	}
	if outcome == session.QuickRunBackendMissing {
		return fmt.Errorf("quick run: backend for shortcut %q (%s) is no longer configured",
			item.Label, item.BaseURL)
	}
	return renderQuickRun(out, errw, g, sess)
}

// newQuickSession builds a production session for a one-shot run: a shared
// httpclient with nav, action and live on top, the same composition
// internal/tui's newProductionSession uses minus the TUI's timer/sender
// wiring (a one-shot has no message loop, so live's default real-time timer
// is fine -- a job an action shortcut starts is reported as started, not
// waited for, per docs/DESIGN.md's "do not claim a job finished").
func newQuickSession() *session.Session {
	client := httpclient.New()
	return session.New(
		nav.New(client),
		action.New(client),
		live.New(client, live.WithLog(func(string) {})),
	)
}

// renderQuickRun prints whatever RunQuick left on the session: a failure
// (State error/unreachable), a question an unsafe action asked, a notice
// (an action that was withdrawn, refused, or succeeded), or the document a
// document shortcut fetched. Exit codes follow the convention: a failure
// maps through exitForFailure (Conflict -> 3, else 1); an unanswered
// question or a non-success notice is a general failure (1); a document or
// a successful action notice is success (0).
func renderQuickRun(out, errw io.Writer, g *globalFlags, sess *session.Session) error {
	if f := sess.Failure(); f != nil {
		return exitForFailure(f)
	}
	if q := sess.Question(); q != nil {
		return renderQuestion(out, g, q)
	}
	if n := sess.Notice(); n != nil {
		return renderNotice(out, errw, g, sess, n)
	}
	return renderQuickDocument(out, g, sess)
}

// renderQuestion prints a question an action shortcut asked, in text or
// JSON. A one-shot cannot answer it (there is no --yes/--value here, by
// design -- answering belongs to `restforge act`), so it is a general
// failure (exit 1) with a message steering the caller there.
func renderQuestion(out io.Writer, g *globalFlags, q session.SessionQuestion) error {
	if g.output == "json" {
		row := questionFields(q)
		row.Kind = "question"
		return PrintJSON(out, row)
	}
	switch qq := q.(type) {
	case session.ConfirmQuestion:
		_, _ = fmt.Fprintf(out, "question: %s\n%s\n", qq.Heading, qq.Body)
	case session.ValueQuestion:
		_, _ = fmt.Fprintf(out, "value needed: %s\n", qq.Label)
	}
	return fmt.Errorf("quick run: this shortcut needs an answer; use `restforge act` to provide it")
}

// renderNotice prints a notice, in text or JSON. ActionOutcomeReported is a
// success (the action was sent and answered, or a job it started was
// accepted); every other notice -- withdrawn, refused, failed, gave up --
// is a failure. A failed action whose cause was a Conflict is a refusal
// (exit 3), matching the convention `restforge act` keeps: the server said
// the state acted on was stale, never retried automatically. A started job
// that is still being watched is reported as started, not waited for
// (docs/DESIGN.md, "do not claim a job finished").
func renderNotice(out, errw io.Writer, g *globalFlags, sess *session.Session, n session.SessionNotice) error {
	if g.output == "json" {
		row := noticeFields(n)
		row.Kind = "notice"
		return PrintJSON(out, row)
	}
	heading, body := noticeText(n)
	_, _ = fmt.Fprint(out, heading, body)
	if sess.IsLive() {
		_, _ = fmt.Fprintln(errw, "job running; quick run does not wait for it (re-run to check progress)")
	}
	if _, ok := n.(session.ActionOutcomeReported); ok {
		return nil
	}
	if failed, ok := n.(session.ActionFailed); ok && failed.Failure != nil && failed.Failure.Kind == failure.Conflict {
		return refusedError(fmt.Errorf("quick run: %s", strings.TrimSpace(heading)))
	}
	return fmt.Errorf("quick run: %s", strings.TrimSpace(heading))
}

// renderQuickDocument prints the document a document shortcut fetched, in
// text (PrintDocumentText, the same helper get uses) or JSON (the raw
// decoded document refetched through the same helper get uses, since the
// session exposes a rendered document, not the raw bytes --output json
// needs).
func renderQuickDocument(out io.Writer, g *globalFlags, sess *session.Session) error {
	doc := sess.Document()
	if g.output == "json" {
		return printQuickDocumentJSON(out, sess)
	}
	return PrintDocumentText(out, doc)
}

// printQuickDocumentJSON refetches the session's current document through
// httpclient to get the raw decoded JSON --output json needs (the session
// holds a rendered document and a typed entity, neither of which preserves
// the server's own shape), then hands it to the same PrintJSON helper get
// uses. A refetch failure is a general failure (exit 1).
func printQuickDocumentJSON(out io.Writer, sess *session.Session) error {
	be := sess.Backend()
	href := sess.Href()
	if href == "" {
		// Nothing was fetched: print an empty document rather than panic on
		// a refetch with no address.
		return PrintJSON(out, map[string]any{})
	}
	raw, _, err := fetchDocument(httpclient.New(), be, href)
	if err != nil {
		return exitForFailure(err)
	}
	return PrintJSON(out, raw)
}

// --- shared helpers ------------------------------------------------------

// resolveQuickItem resolves arg to an index into items: a non-negative
// integer is an index (0-based, matching quick.Get/quick.Remove); any other
// value is a label, which must match exactly one shortcut. A label that
// matches nothing or more than one is a config-state failure (exit 1); an
// out-of-range index is too.
func resolveQuickItem(items []quick.QuickItem, arg string) (int, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 0 || n >= len(items) {
			return 0, fmt.Errorf("quick: no shortcut at index %d (have %d)", n, len(items))
		}
		return n, nil
	}
	return resolveByLabel(items, arg)
}

// resolveByLabel finds the index of the unique shortcut whose label matches
// arg. Zero matches is not-found; more than one is ambiguous -- both are
// config-state failures (exit 1), with a message that names the count so a
// caller knows whether to disambiguate by index.
func resolveByLabel(items []quick.QuickItem, label string) (int, error) {
	first := -1
	count := 0
	for i, item := range items {
		if item.Label == label {
			if count == 0 {
				first = i
			}
			count++
		}
	}
	switch count {
	case 0:
		return 0, fmt.Errorf("quick: no shortcut labeled %q", label)
	case 1:
		return first, nil
	default:
		return 0, fmt.Errorf("quick: %d shortcuts labeled %q; disambiguate by index", count, label)
	}
}

// quickRunJSON is the JSON shape of a non-document `quick run` result -- a
// question or a notice. A document result prints the raw decoded document
// straight through (see renderQuickDocument), the same shape get/act use.
type quickRunJSON struct {
	Kind    string `json:"kind"`
	Heading string `json:"heading,omitempty"`
	Body    string `json:"body,omitempty"`
	Message string `json:"message,omitempty"`
	Label   string `json:"label,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// questionFields returns the kind-specific JSON fields for a question, as a
// zero-value quickRunJSON each branch fills in. Kept as a helper so
// renderQuestion's JSON branch reads as one PrintJSON call.
func questionFields(q session.SessionQuestion) quickRunJSON {
	switch qq := q.(type) {
	case session.ConfirmQuestion:
		return quickRunJSON{Heading: qq.Heading, Body: qq.Body}
	case session.ValueQuestion:
		return quickRunJSON{Label: qq.Label}
	}
	return quickRunJSON{}
}

// noticeFields returns the kind-specific JSON fields for a notice. Split out
// of renderNotice so the type switch reads as one unit.
func noticeFields(n session.SessionNotice) quickRunJSON {
	switch nn := n.(type) {
	case session.ActionWithdrawn:
		return quickRunJSON{Heading: nn.Heading, Reason: "action no longer offered"}
	case session.ActionRefused:
		return quickRunJSON{Heading: nn.Heading, Reason: nn.Reason}
	case session.ActionOutcomeReported:
		return quickRunJSON{Heading: nn.Heading, Message: nn.Message, Body: nn.Body}
	case session.ActionFailed:
		if nn.Failure != nil {
			return quickRunJSON{Heading: nn.Heading, Reason: nn.Failure.Error()}
		}
		return quickRunJSON{Heading: nn.Heading}
	case session.ActionGaveUp:
		return quickRunJSON{Heading: nn.Heading, Reason: "gave up watching before the job finished"}
	}
	return quickRunJSON{}
}

// noticeText returns a heading and body for a notice's text rendering. The
// heading names the outcome; the body carries the detail (a message, a
// reason, or the rendered result), with a trailing newline so it reads as a
// block. ActionProgress should not appear after RunQuick (no watch is
// driven here), so it falls through to a generic heading.
func noticeText(n session.SessionNotice) (string, string) {
	switch nn := n.(type) {
	case session.ActionWithdrawn:
		return fmt.Sprintf("action %q is no longer offered\n", nn.Heading), ""
	case session.ActionRefused:
		return fmt.Sprintf("action %q refused: %s\n", nn.Heading, nn.Reason), ""
	case session.ActionOutcomeReported:
		heading := fmt.Sprintf("%s: %s\n", nn.Heading, nn.Message)
		return heading, nn.Body + "\n"
	case session.ActionFailed:
		reason := "unknown"
		if nn.Failure != nil {
			reason = nn.Failure.Error()
		}
		return fmt.Sprintf("action %q failed: %s\n", nn.Heading, reason), ""
	case session.ActionGaveUp:
		return fmt.Sprintf("gave up watching %q before it finished\n", nn.Heading), ""
	case session.ActionProgress:
		return fmt.Sprintf("%s: %s\n", nn.Heading, nn.Step), ""
	}
	return fmt.Sprintf("%v\n", n), ""
}
