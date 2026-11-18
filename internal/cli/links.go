package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newLinksCmd(a *app) *cobra.Command {
	var (
		l                    listFlags
		status, repo, domain string
	)
	list := &cobra.Command{
		Use:   "list",
		Short: "Which repository deploys or defines which host",
		Long: "The links between repositories and your domains' hosts: what HKr\n" +
			"proposed and what people decided. Fixes open pull requests on the\n" +
			"repository linked to the finding's host.",
		Example: "  hkr links list --status proposed\n  hkr links list --repo acme/web",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := oneOf("--status", status, "proposed", "confirmed", "rejected"); err != nil {
				return err
			}
			ctx := cmd.Context()
			cl, org, err := a.inOrg(ctx)
			if err != nil {
				return err
			}
			q := api.Query{"status": status}
			if repo != "" {
				if q["repo"], err = a.repoID(ctx, cl, org, repo); err != nil {
					return err
				}
			}
			if domain != "" {
				if q["domain"], err = a.domainID(ctx, cl, org, domain); err != nil {
					return err
				}
			}
			return printList(ctx, a, cl, &l, api.OrgPath(org)+"/links", q, linksTable, "No links.")
		},
	}
	list.Flags().StringVar(&status, "status", "", "proposed, confirmed or rejected")
	list.Flags().StringVar(&repo, "repo", "", "only this repository (full name or id)")
	list.Flags().StringVar(&domain, "domain", "", "only this domain's hosts")
	l.add(list)

	var kind string
	add := &cobra.Command{
		Use:   "add <repo> <host>",
		Short: "Link a repository to a host, by hand",
		Long: "Say a repository deploys (or defines) one of your domains' hosts, and\n" +
			"confirm it. --kind is deploys (the default), defines or unknown.",
		Example: "  hkr links add acme/web api.shop.example.com\n  hkr links add acme/infra shop.example.com --kind defines",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if kind == "" {
				kind = "deploys"
			}
			if err := oneOf("--kind", kind, "deploys", "defines", "unknown"); err != nil {
				return err
			}
			ctx := cmd.Context()
			cl, org, err := a.inOrg(ctx)
			if err != nil {
				return err
			}
			var created api.Link
			if _, err := cl.Send(ctx, http.MethodPost, api.OrgPath(org)+"/links", nil,
				map[string]string{"repo": args[0], "target": args[1], "kind": kind}, &created); err != nil {
				return err
			}
			return a.decideLink(ctx, cl, org, created.ID, "confirm")
		},
	}
	add.Flags().StringVar(&kind, "kind", "", "deploys (default), defines or unknown")

	decide := func(verb, short string) *cobra.Command {
		return &cobra.Command{
			Use:     verb + " <id>…",
			Short:   short,
			Example: "  hkr links " + verb + " Ln2Kq2XpAb2m",
			Args:    argsBetween(1, 100),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				cl, org, err := a.inOrg(ctx)
				if err != nil {
					return err
				}
				for _, id := range args {
					if err := a.decideLink(ctx, cl, org, id, verb); err != nil {
						return err
					}
				}
				return nil
			},
		}
	}
	return group(a, "links", "Which repository deploys which host (organization admins)",
		"  hkr links list --status proposed\n  hkr links confirm Ln2Kq2XpAb2m\n  hkr links add acme/web api.shop.example.com",
		list, add, decide("confirm", "Confirm links HKr proposed"), decide("reject", "Reject links HKr proposed"))
}

// decideLink confirms or rejects a link and says so.
func (a *app) decideLink(ctx context.Context, cl *api.Client, org, id, verb string) error {
	var link api.Link
	raw, err := cl.Send(ctx, http.MethodPost, api.OrgPath(org)+"/links/"+api.Seg(id)+"/"+verb, nil, nil, &link)
	if err != nil {
		return err
	}
	if a.flags.json {
		return a.printJSON(raw)
	}
	a.out("%s: %s %s %s.", upperFirst(link.Status), render.Safe(link.Repository.FullName), link.Kind, render.Safe(link.Target.Value))
	a.nextStep(ctx, cl, org)
	return nil
}

func linksTable(ls []api.Link) string {
	rows := make([][]string, 0, len(ls))
	for _, l := range ls {
		source := l.Source
		if l.Confidence != nil {
			source += fmt.Sprintf(" (%d%%)", *l.Confidence)
		}
		rows = append(rows, []string{l.ID, l.Status, render.Safe(l.Repository.FullName), render.Safe(l.Target.Value), l.Kind, source})
	}
	return render.Table([]string{"ID", "STATUS", "REPOSITORY", "HOST", "KIND", "SOURCE"}, rows)
}
