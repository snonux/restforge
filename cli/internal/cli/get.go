package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/siren"
)

// newGetCmd builds the `restforge get` one-shot subcommand: fetch a Siren
// document and print it. With no argument it opens the selected backend's
// root; an argument is an href resolved against that backend exactly as
// internal/nav resolves one (httpclient.Get, the layer nav itself uses);
// --rel follows the link with that rel from the root, the CLI-mode
// equivalent of backend.Backend.StartRel. --output selects text (rows, the
// default) or json (the raw decoded document).
func newGetCmd(g *globalFlags) *cobra.Command {
	var rel string
	cmd := &cobra.Command{
		Use:   "get [HREF]",
		Short: "fetch and print a Siren document",
		Long: "Fetch a Siren document and print it. With no HREF, opens the\n" +
			"selected backend's root; --rel follows the link with that rel from\n" +
			"the root instead. --output text (the default) prints one row per\n" +
			"line; --output json prints the raw decoded document for piping into jq.",
		// A custom Args validator (rather than cobra.MaximumNArgs) so a too-many-
		// args mistake surfaces as a usage error (exit 2), not a plain Cobra
		// error that would fall through to general failure (exit 1).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageErrorf("get takes at most one HREF argument, got %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.OutOrStdout(), g, args, rel)
		},
	}
	cmd.Flags().StringVar(&rel, "rel", "",
		"follow the link with this rel from the backend's root, instead of an HREF")
	return cmd
}

// runGet is the get command's body, split out of the RunE closure so it
// reads as straight-line logic and a test can call it with a buffer.
func runGet(out io.Writer, g *globalFlags, args []string, rel string) error {
	backends, err := config.LoadBackends()
	if err != nil {
		return fmt.Errorf("get: could not load backends: %w", err)
	}
	be, err := selectBackend(backends, g.backendName)
	if err != nil {
		return err
	}

	client := httpclient.New()
	href, err := getTargetHref(client, be, args, rel)
	if err != nil {
		return err
	}

	raw, entity, err := fetchDocument(client, be, href)
	if err != nil {
		return exitForFailure(err)
	}

	if g.output == "json" {
		return PrintJSON(out, raw)
	}
	doc := render.Document(&entity, href, be)
	return PrintDocumentText(out, &doc)
}

// getTargetHref resolves what href to fetch: an explicit HREF argument, the
// backend's root when neither HREF nor --rel was given, or the href the
// root links at --rel when --rel was given. --rel and an HREF argument are
// mutually exclusive (a usage error), since both name a destination.
func getTargetHref(client *httpclient.Client, be backend.Backend, args []string, rel string) (string, error) {
	if rel != "" {
		if len(args) > 0 {
			return "", usageErrorf("--rel and an HREF argument are mutually exclusive")
		}
		return resolveRel(client, be, rel)
	}
	switch len(args) {
	case 0:
		return be.BaseURL, nil
	case 1:
		return args[0], nil
	default:
		// Defensive: the Args validator above already rejects >1, so this
		// only guards a caller that invokes getTargetHref directly.
		return "", usageErrorf("get takes at most one HREF argument, got %d", len(args))
	}
}

// resolveRel fetches the backend's root and follows the link with rel on it,
// returning that link's href -- the same primitive nav.followStart uses
// (siren.Entity.Follow), so the resolution is identical. An empty href
// means the root does not offer the rel, a real answer reported as a
// general failure rather than routed around.
func resolveRel(client *httpclient.Client, be backend.Backend, rel string) (string, error) {
	_, root, err := fetchDocument(client, be, be.BaseURL)
	if err != nil {
		return "", exitForFailure(err)
	}
	href := root.Follow(rel)
	if href == "" {
		return "", fmt.Errorf("get: backend %q does not offer rel %q from its root", be.Name, rel)
	}
	return href, nil
}

// fetchDocument performs one GET against be for href (resolved against
// be's base by httpclient, the same layer nav uses), decodes the Siren
// entity and applies the version check nav applies, returning both the
// raw decoded document (for --output json) and the typed entity (for the
// text rendering). A transport or server failure comes back as a
// *failure.Failure for [exitForFailure] to map onto an exit code.
func fetchDocument(client *httpclient.Client, be backend.Backend, href string) (any, siren.Entity, error) {
	resp, err := client.Get(be, href)
	if err != nil {
		return nil, siren.Entity{}, err
	}
	entity := siren.EntityFromJSON(resp.Entity)
	if problem := entity.VersionProblem(); problem != "" {
		return nil, siren.Entity{}, &failure.Failure{Kind: failure.Client, Message: problem}
	}
	return resp.Entity, entity, nil
}
