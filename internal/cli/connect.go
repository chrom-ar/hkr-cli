package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

const (
	// connectWait is how long repos connect waits for the connection.
	connectWait = 15 * time.Minute
	// connectPoll is how often it looks.
	connectPoll = 3 * time.Second
)

func newReposConnectCmd(a *app) *cobra.Command {
	var noBrowser, noWait bool
	c := &cobra.Command{
		Use:   "connect github|gitlab|bitbucket",
		Short: "Connect a git host, in the browser (organization admins)",
		Long: "Connect GitHub, GitLab or Bitbucket to the organization, so HKr can\n" +
			"review pushes and open fix pull requests. It opens HKr's page in the\n" +
			"browser, where an admin of the organization, signed in, authorizes it;\n" +
			"then it waits for the connection and lists the repositories. --no-wait\n" +
			"only prints the page.",
		Example: "  hkr repos connect github\n  hkr repos connect gitlab --no-browser",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("provider", args[0], "github", "gitlab", "bitbucket"); err != nil {
				return err
			}
			ctx := cmd.Context()
			cl, org, err := a.inOrg(ctx)
			if err != nil {
				return err
			}
			before := map[string]bool{}
			if !noWait {
				page, err := api.List[api.Connection](ctx, cl, api.OrgPath(org)+"/connections", nil, true)
				if err != nil {
					return err
				}
				for _, c := range page.Items {
					before[c.ID] = true
				}
			}
			link := cl.Origin() + "/vcs/" + args[0] + "/connect?org=" + url.QueryEscape(org)
			if a.flags.json {
				fmt.Fprintln(a.env.Stderr, link) // stdout is only HKr's JSON.
			} else {
				a.out("%s", link)
			}
			a.progress("Open it in a browser where an admin of %s is signed in: it needs an admin of %s signed in.", render.Safe(org), render.Safe(org))
			if a.env.StdoutIsTerminal && !noBrowser {
				_ = a.env.OpenBrowser(link)
			}
			if noWait {
				return nil
			}
			a.progress("Waiting for the connection… (Ctrl-C stops waiting)")
			conn, err := a.waitConnection(ctx, cl, org, before, link)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if err != nil {
				return err
			}
			if !a.flags.json {
				a.out("Connected %s (%s).", render.Safe(conn.Provider), render.Safe(conn.DisplayName))
			}
			var l listFlags
			if err := printList(ctx, a, cl, &l, api.OrgPath(org)+"/repos", nil, reposTable,
				"HKr sees no repositories yet; it reads them in the background: hkr repos list"); err != nil {
				return err
			}
			a.nextStep(ctx, cl, org)
			return nil
		},
	}
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the page")
	c.Flags().BoolVar(&noWait, "no-wait", false, "don't wait for the connection")
	return c
}

// waitConnection polls the organization's connections until one that
// wasn't there before is active, for up to connectWait.
func (a *app) waitConnection(ctx context.Context, cl *api.Client, org string, before map[string]bool, link string) (*api.Connection, error) {
	sleep := a.env.Sleep
	if sleep == nil {
		sleep = api.Sleep
	}
	deadline := a.env.Now().Add(connectWait)
	for {
		if err := sleep(ctx, connectPoll); err != nil {
			return nil, err
		}
		page, err := api.List[api.Connection](ctx, cl, api.OrgPath(org)+"/connections", nil, true)
		if err != nil {
			return nil, err
		}
		for _, c := range page.Items {
			if !before[c.ID] && c.Status == "active" {
				return &c, nil
			}
		}
		if !a.env.Now().Before(deadline) {
			return nil, fmt.Errorf("no connection appeared in %s; open the URL again: %s", durationText(connectWait), link)
		}
	}
}
