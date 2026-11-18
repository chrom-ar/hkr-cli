package cli

import (
	"errors"
	"strconv"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/target"
	"github.com/spf13/cobra"
)

func newOpenCmd(a *app) *cobra.Command {
	var noBrowser bool
	c := &cobra.Command{
		Use:   "open [findings | finding <id|url> | domain <domain> | scan <domain> [<number>] | repo <repo>]",
		Short: "Open the web app's page for a finding, domain, scan or repository",
		Long: "Open a page of HKr's web app: home (no argument), the findings, one\n" +
			"finding, a domain, a scan, or a repository. The URL is printed too. The\n" +
			"web app shows the organization you last chose there.",
		Example: "  hkr open\n" +
			"  hkr open finding Fx7Kq2XpAb2m\n" +
			"  hkr open scan shop.example.com 3\n" +
			"  hkr open repo acme/web --no-browser",
		Args: argsBetween(0, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			page := argAt(args, 0)
			want := map[string][2]int{"": {0, 0}, "findings": {0, 0}, "finding": {1, 1}, "domain": {1, 1}, "scan": {1, 2}, "repo": {1, 1}}
			n, ok := want[page]
			if !ok {
				return usageError{errors.New("unknown page " + quote(page) + ": use findings, finding, domain, scan or repo")}
			}
			rest := args[min(1, len(args)):]
			if len(rest) < n[0] || len(rest) > n[1] {
				return usageError{errors.New("usage: hkr open " + page + " " + map[string]string{
					"finding": "<id|url>", "domain": "<domain>", "scan": "<domain> [<number>]", "repo": "<repo>"}[page])}
			}
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			link := cl.Origin() + "/"
			switch page {
			case "findings":
				link += "findings"
			case "finding":
				id, err := target.ParseFinding(rest[0], cl.Origin())
				if err != nil {
					return err
				}
				link += "findings/" + api.Seg(id)
			case "domain", "scan":
				_, org, id, err := a.inDomain(ctx, rest[0])
				if err != nil {
					return err
				}
				link += "domains/" + api.Seg(id)
				if page == "scan" {
					number := 0
					if len(rest) == 2 {
						if number, err = parseNumber(rest[1]); err != nil {
							return err
						}
					} else {
						s, _, err := cl.Scan(ctx, org, id, 0)
						if err != nil {
							return err
						}
						number = s.Number
					}
					link += "/scans/" + strconv.Itoa(number)
				}
			case "repo":
				_, _, id, err := a.inRepo(ctx, rest[0])
				if err != nil {
					return err
				}
				link += "code/" + api.Seg(id)
			}
			a.out("%s", link)
			if a.env.StdoutIsTerminal && !noBrowser {
				_ = a.env.OpenBrowser(link)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the URL")
	return c
}

func quote(s string) string { return `"` + s + `"` }
