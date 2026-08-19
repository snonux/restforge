package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/snonux/restforge/cli/internal/action"
	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/live"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/siren"
)

// newActCmd builds the `restforge act` one-shot subcommand: invoke a Siren
// action without an interactive prompt. ACTION-NAME names the action; --on
// HREF selects the document it lives on (the backend's root by default);
// --field key=value supplies field values; --yes confirms an unsafe action
// (without --yes an unsafe action is refused and nothing is sent -- the
// "ask before acting" rule, docs/DESIGN.md, translated to a non-interactive
// context); --value answers the single required field that has no default
// and no --field.
func newActCmd(g *globalFlags) *cobra.Command {
	var on, value string
	var yes bool
	var fields []string
	cmd := &cobra.Command{
		Use:   "act ACTION-NAME [--on HREF] [--field key=value]... [--yes] [--value TEXT]",
		Short: "invoke a Siren action one-shot",
		Long: "Invoke a Siren action and print the result. ACTION-NAME is the\n" +
			"action's name on the document at --on HREF (the backend's root by\n" +
			"default). Unsafe actions are refused unless --yes is given; a\n" +
			"required field with no default and no --field is answered by --value,\n" +
			"or printed and left unfilled if --value is absent. A watchable (202)\n" +
			"response is followed synchronously, progress to stderr, result to\n" +
			"stdout.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageErrorf("act takes exactly one ACTION-NAME, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAct(cmd.OutOrStdout(), cmd.ErrOrStderr(), g,
				args[0], on, fields, yes, value)
		},
	}
	cmd.Flags().StringVar(&on, "on", "",
		"href of the document the action is on (default: the backend's root)")
	cmd.Flags().StringArrayVar(&fields, "field", nil,
		"field value as key=value (repeatable)")
	cmd.Flags().BoolVar(&yes, "yes", false,
		"confirm an unsafe action without prompting; without --yes it is refused and nothing is sent")
	cmd.Flags().StringVar(&value, "value", "",
		"value for the single required field that has no default and no --field")
	return cmd
}

// runAct is the act command's body, split out of the RunE closure so it
// reads as straight-line logic and a test can call it with buffers. errw is
// where live-watch progress (and the gave-up note) go; command failures come
// back as the returned error, which the dispatch layer prints to errw too.
func runAct(out, errw io.Writer, g *globalFlags, name, on string, fields []string, yes bool, value string) error {
	backends, err := config.LoadBackends()
	if err != nil {
		return fmt.Errorf("act: could not load backends: %w", err)
	}
	be, err := selectBackend(backends, g.backendName)
	if err != nil {
		return err
	}

	userValues, err := parseFields(fields)
	if err != nil {
		return err
	}

	client := httpclient.New()
	originHref := on
	if originHref == "" {
		originHref = be.BaseURL
	}
	_, origin, err := fetchDocument(client, be, originHref)
	if err != nil {
		return exitForFailure(err)
	}

	actions := action.New(client, action.WithUserValues(userValues))
	return driveAct(out, errw, g, client, be, actions, origin, name, yes, value)
}

// driveAct runs the Ask -> Answer/AnswerValue state machine the interactive
// TUI walks one question at a time, but straight through: Ask either
// invokes a safe action outright, or reports an unsafe one needs
// confirmation (refused here without --yes, sent with it). The resulting
// InvokeOutcome is handed to handleInvoke, which may recurse once into
// AnswerValue when exactly one field is still outstanding.
func driveAct(out, errw io.Writer, g *globalFlags, client *httpclient.Client,
	be backend.Backend, actions *action.Action, origin siren.Entity, name string, yes bool, value string) error {
	switch o := actions.Ask(be, origin, name).(type) {
	case action.ActionNotOffered:
		return fmt.Errorf("act: action %q is not offered by %s", o.Name, docAt(be, origin))
	case action.ConfirmationRequired:
		if !yes {
			// Ask before acting, non-interactive: never send an unsafe action
			// the caller did not explicitly confirm. A withdrawal, exit 3.
			return refusedError(fmt.Errorf(
				"%s\n\n%s\n\nRefusing: %q is unsafe and --yes was not given. Re-run with --yes to confirm and send it.",
				o.Heading, o.Body, name))
		}
		return handleInvoke(out, errw, g, client, be, actions, origin, actions.Answer(true, be, origin), value)
	case action.ActionInvoked:
		// A safe method went straight through; no --yes needed.
		return handleInvoke(out, errw, g, client, be, actions, origin, o.Outcome, value)
	}
	return nil
}

// handleInvoke turns one InvokeOutcome into output or a follow-up
// AnswerValue call. Split out of driveAct so the unsafe/safe branches share
// one place that decides what an invocation produced.
func handleInvoke(out, errw io.Writer, g *globalFlags, client *httpclient.Client,
	be backend.Backend, actions *action.Action, origin siren.Entity, outcome action.InvokeOutcome, value string) error {
	switch o := outcome.(type) {
	case action.InvokeRefused:
		return fmt.Errorf("act: %s", o.Reason)
	case action.InvokeNeedsValue:
		if value == "" {
			// A script expects a deterministic exit, not a blocked stdin; the
			// interactive prompt belongs to the TUI, not this command.
			return fmt.Errorf("act: action needs a value for %q (re-run with --value TEXT or --field %s=VALUE)",
				o.Label, o.FieldName)
		}
		return handleInvoke(out, errw, g, client, be, actions, origin, actions.AnswerValue(value, be, origin), "")
	case action.InvokeSucceeded:
		return handleSuccess(out, errw, g, client, be, origin, o.Response)
	case action.InvokeFailed:
		return exitForFailure(o.Failure)
	}
	return nil
}

// handleSuccess prints a non-watchable response straight away, or hands a
// watchable (202) one to the blocking live watch. Split out so the
// watchable/non-watchable decision reads as one line.
func handleSuccess(out, errw io.Writer, g *globalFlags, client *httpclient.Client,
	be backend.Backend, origin siren.Entity, resp httpclient.HTTPResponse) error {
	resultEntity := siren.EntityFromJSON(resp.Entity)
	if problem := resultEntity.VersionProblem(); problem != "" {
		return &failure.Failure{Kind: failure.Client, Message: problem}
	}
	result := live.ActionOutcome{Status: resp.Status, Entity: resultEntity}
	if !live.ShouldWatch(result) {
		return printResult(out, g, resp.Entity, resultEntity, "")
	}
	return watchAndPrint(out, errw, g, client, be, origin, result, resp.Entity)
}

// watchAndPrint blocks on the live poll (live.WaitForLive, the variant
// internal/session's package comment names for this caller), printing each
// progress step to errw as it arrives, then prints the final result. The
// final document is re-fetched from the poll target so --output json gets the
// raw decoded document (WaitForLive returns a typed entity, not the raw
// bytes); when there was nothing to follow, the immediate response stands in.
func watchAndPrint(out, errw io.Writer, g *globalFlags, client *httpclient.Client,
	be backend.Backend, origin siren.Entity, result live.ActionOutcome, fallbackRaw any) error {
	l := live.New(client, live.WithLog(func(string) {}))
	finalEntity, done, _ := l.WaitForLive(be,
		live.Origin{Entity: origin}, result,
		func(e siren.Entity) { _, _ = fmt.Fprintln(errw, live.ProgressText(e)) })
	if !done {
		_, _ = fmt.Fprintln(errw, "gave up watching; the job may still be running")
	}

	raw, entity := fallbackRaw, finalEntity
	if href := live.PollTarget(origin, result.Entity); href != "" {
		if r, e, err := fetchDocument(client, be, href); err == nil {
			raw, entity = r, e
		}
	}
	return printResult(out, g, raw, entity, "result")
}

// printResult writes the result in the selected format: text rows (the
// default) or the raw decoded document for --output json. nil raw falls
// back to re-marshalling the typed entity, which only happens when no raw
// document was available (nothing to watch and nothing to follow).
func printResult(out io.Writer, g *globalFlags, raw any, entity siren.Entity, fallback string) error {
	if g.output == "json" {
		if raw == nil {
			return PrintJSON(out, entity)
		}
		return PrintJSON(out, raw)
	}
	doc := render.Document(&entity, fallback)
	return PrintDocumentText(out, &doc)
}

// parseFields turns the repeatable --field key=value flags into the map
// action.WithUserValues consumes. A missing '=' or an empty key is a usage
// error (exit 2): the caller typed the flag wrong, nothing reached the
// server.
func parseFields(fields []string) (map[string]string, error) {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return nil, usageErrorf("invalid --field %q (want key=value)", f)
		}
		values[k] = v
	}
	return values, nil
}

// docAt is a short "at <href>" suffix for the not-offered message, empty
// when the origin carried no href (an embedded sub-entity). Kept here so the
// not-offered error reads naturally without action- or nav-specific
// vocabulary.
func docAt(be backend.Backend, origin siren.Entity) string {
	if href := origin.Follow("self"); href != "" {
		return be.Name + " at " + href
	}
	return be.Name
}
