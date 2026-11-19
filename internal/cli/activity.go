package cli

import (
	"fmt"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

// newActivityCmd is hkr activity, and with repo set, hkr repos activity
// <repo>.
func newActivityCmd(a *app, perRepo bool) *cobra.Command {
	var (
		l          listFlags
		repo, kind string
	)
	c := &cobra.Command{
		Use:   "activity",
		Short: "What HKr did with your code, newest first (organization admins)",
		Long: "Every delivery from the git host and what HKr decided (the review it\n" +
			"started, or why it skipped), every pull request HKr opened, and every\n" +
			"read of the lock files. --kind is delivery, pull_request or\n" +
			"dependencies.",
		Example: "  hkr activity\n  hkr activity --repo acme/web --kind pull_request",
		Args:    noArgs,
	}
	if perRepo {
		c.Use, c.Short = "activity <repo>", "What HKr did with one repository, newest first"
		c.Long = strings.Replace(c.Long, "Every", "In the repository: every", 1)
		c.Example = "  hkr repos activity acme/web"
		c.Args = argsBetween(1, 1)
	} else {
		c.Flags().StringVar(&repo, "repo", "", "only this repository")
	}
	c.Flags().StringVar(&kind, "kind", "", "only delivery, pull_request or dependencies")
	l.add(c)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if kind != "" {
			if err := oneOf("--kind", kind, "delivery", "pull_request", "dependencies"); err != nil {
				return err
			}
		}
		if perRepo {
			repo = args[0]
		}
		ctx := cmd.Context()
		cl, org, err := a.inOrg(ctx)
		if err != nil {
			return err
		}
		q := api.Query{}
		if kind != "" {
			q["kind"] = kind
		}
		if repo != "" {
			if q["repo"], err = a.repoID(ctx, cl, org, repo); err != nil {
				return err
			}
		}
		return printList(ctx, a, cl, &l, api.OrgPath(org)+"/activity", q, func(as []api.Activity) string {
			rows := make([][]string, 0, len(as))
			for _, e := range as {
				rows = append(rows, []string{render.Time(e.At, a.env.Location, "2 Jan 2006 15:04"),
					render.Safe(e.Repository.FullName), activityText(&e)})
			}
			return render.Table([]string{"WHEN", "REPOSITORY", "WHAT"}, rows)
		}, "No activity yet.")
	}
	return c
}

// activityText is one entry in a line: "push main a3f21c9d8e7b by ana →
// reviewed (1 comment)", "PR #412 open, verified · fixes F-07", "lock
// files main a1b2c3d4e5f6 · 3/3 read".
func activityText(e *api.Activity) string {
	where := strings.TrimSpace(render.Safe(e.Branch) + " " + shortSHA(render.Safe(e.CommitSHA)))
	switch e.Kind {
	case "delivery":
		s := strings.TrimSpace(render.Safe(e.Event) + " " + where)
		if e.PushedBy != "" {
			s += " by " + render.Safe(e.PushedBy)
		}
		s += " → " + render.Safe(e.Outcome)
		switch {
		case e.Reason != "":
			s += " (" + render.Safe(e.Reason) + ")"
		case e.Review != nil && e.Review.Status != "completed":
			s += " (review " + render.Safe(e.Review.Status) + ")"
		case e.Review != nil:
			s += fmt.Sprintf(" (%d comment%s)", e.Review.Comments, plural(e.Review.Comments))
		}
		return s
	case "pull_request":
		s := fmt.Sprintf("PR #%d %s", e.Number, render.Safe(e.State))
		if e.Status != "" {
			s += ", " + render.Safe(e.Status)
		}
		switch {
		case e.Finding != nil:
			s += " · fixes " + render.Safe(e.Finding.Ref)
		case e.Dependency != nil:
			s += " · upgrades " + render.Safe(e.Dependency.PackageName)
		}
		return s
	case "dependencies":
		s := "lock files " + where
		if e.Status == "complete" {
			return s + fmt.Sprintf(" · %d/%d read", e.ProjectsRead, e.ProjectsRead+e.ProjectsUnread)
		}
		return s + " · " + render.Safe(e.Status)
	}
	return render.Safe(e.Kind)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
