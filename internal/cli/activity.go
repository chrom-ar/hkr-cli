package cli

import (
	"context"
	"fmt"
	"strconv"
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
	if !perRepo {
		c.AddCommand(newActivityShowCmd(a))
	}
	return c
}

func (a *app) activity(ctx context.Context, id string) (*api.ActivityDetail, []byte, error) {
	cl, org, err := a.inOrg(ctx)
	if err != nil {
		return nil, nil, err
	}
	var e api.ActivityDetail
	raw, err := cl.Get(ctx, api.OrgPath(org)+"/activity/"+api.Seg(id), nil, &e)
	if err != nil {
		return nil, nil, err
	}
	return &e, raw, nil
}

func newActivityShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "One entry, as its page in the web app shows it",
		Long: "One entry of hkr activity: a delivery and what HKr decided, a fix pull\n" +
			"request with its test runs and its whole patch, or a read of the lock\n" +
			"files with each project. hkr open activity <id> opens its page.",
		Example: "  hkr activity show pull_request-Fx2Kq2XpAb2m\n" +
			"  hkr activity show dependencies-Sy2Kq2XpAb2m --json",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, raw, err := a.activity(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.showActivity(e)
			return nil
		},
	}
}

func (a *app) showActivity(e *api.ActivityDetail) {
	d := &e.Details
	repo := render.Safe(e.Repository.FullName)
	line := func(width int, label, v string) {
		if v != "" {
			a.out("%-*s%s", width, label, v)
		}
	}
	switch e.Kind {
	case "delivery":
		a.out("%s on %s, %s", deliveryText(&e.Activity), repo, render.Time(e.At, a.env.Location, "2 Jan 2006 15:04"))
		line(11, "Outcome", render.Safe(e.Outcome)+outcomeNote(&e.Activity))
		if e.Review != nil {
			line(11, "Review", e.Review.ID+": hkr reviews show "+repo+" "+e.Review.ID)
		}
		line(11, "Host", render.Safe(d.Host))
		line(11, "Delivery", render.Safe(d.DeliveryID))
	case "pull_request":
		head := fmt.Sprintf("PR #%d on %s: %s", e.Number, repo, render.Safe(e.State))
		if e.Status != "" {
			head += ", " + render.Safe(e.Status)
		}
		switch {
		case e.Finding != nil:
			head += " · fixes " + render.Safe(e.Finding.Ref+" "+e.Finding.Title)
		case e.Dependency != nil:
			dep := e.Dependency
			head += " · upgrades " + render.Safe(dep.PackageName+" "+dep.MatchedVersion+" → "+dep.FixedVersion)
		}
		a.out("%s", head)
		line(14, "Pull request", render.Safe(e.PullRequestURL))
		line(14, "Branch", render.Safe(d.Branch))
		if d.OpenedBy != nil {
			line(14, "Opened by", render.Safe(d.OpenedBy.Name))
		} else {
			line(14, "Opened by", "HKr, under the fix policy")
		}
		line(14, "Tests", cmdRunText(d.Tests))
		line(14, "Reproduction", cmdRunText(d.Reproduction))
		if e.Dependency != nil {
			line(14, "Upgraded", bumpedText(e.Dependency.PackageName, d.BumpedVersions))
			if e.Dependency.ID != "" {
				line(14, "CVE", "hkr deps show "+render.Safe(e.Dependency.ID))
			}
		}
		line(14, "Unverified", render.Safe(d.UnverifiedReason))
		line(14, "Merged as", shortSHA(render.Safe(d.MergeCommitSHA)))
		if d.SettledAt != "" {
			line(14, "Settled", render.Time(d.SettledAt, a.env.Location, "2 Jan 2006 15:04"))
		}
		if d.Patch != "" {
			a.out("\n%s", strings.TrimRight(render.SafeText(d.Patch), "\n"))
		}
	case "dependencies":
		status := render.Safe(e.Status)
		if e.Status == "complete" {
			status += ", " + readText(&e.Activity)
		}
		a.out("lock files %s on %s: %s", where(&e.Activity), repo, status)
		line(10, "Started", render.Time(d.StartedAt, a.env.Location, "2 Jan 2006 15:04"))
		if e.FinishedAt != "" {
			line(10, "Finished", render.Time(e.FinishedAt, a.env.Location, "2 Jan 2006 15:04"))
		}
		if len(d.Projects) > 0 {
			rows := make([][]string, 0, len(d.Projects))
			for _, p := range d.Projects {
				rows = append(rows, []string{render.Safe(p.Path), render.Safe(strings.Join(p.Ecosystems, ", ")), strconv.Itoa(p.Packages)})
			}
			a.out("\n%s", render.Table([]string{"PATH", "ECOSYSTEMS", "PACKAGES"}, rows))
		}
		if len(d.Unread) > 0 {
			a.out("\nNot read:")
			for _, u := range d.Unread {
				text := render.Safe(u.Path) + "  " + render.Safe(u.Reason)
				if u.Detail != "" {
					text += ": " + render.Safe(u.Detail)
				}
				a.out("  %s", text)
			}
		}
	default:
		a.out("%s on %s", render.Safe(e.Kind), repo)
	}
}

// outcomeNote is what follows a delivery's outcome: why it skipped, or
// the review it started.
func outcomeNote(e *api.Activity) string {
	switch {
	case e.Reason != "":
		return " (" + render.Safe(e.Reason) + ")"
	case e.Review != nil && e.Review.Status != "completed":
		return " (review " + render.Safe(e.Review.Status) + ")"
	case e.Review != nil:
		return fmt.Sprintf(" (%d comment%s)", e.Review.Comments, plural(e.Review.Comments))
	}
	return ""
}

// cmdRunText is "mix test: exit 1 → 0"; "" when HKr ran nothing.
func cmdRunText(r api.CmdRun) string {
	if r.Command == "" {
		return ""
	}
	exit := func(p *int) string {
		if p == nil {
			return "–"
		}
		return strconv.Itoa(*p)
	}
	return render.Safe(r.Command) + ": exit " + exit(r.ExitBefore) + " → " + exit(r.ExitAfter)
}

// activityText is one entry in a line: "push main a3f21c9d8e7b by ana →
// reviewed (1 comment)", "PR #412 open, verified · fixes F-07", "lock
// files main a1b2c3d4e5f6 · 3/3 read".
func activityText(e *api.Activity) string {
	switch e.Kind {
	case "delivery":
		return deliveryText(e) + " → " + render.Safe(e.Outcome) + outcomeNote(e)
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
		return "lock files " + where(e) + " · " + readText(e)
	}
	return render.Safe(e.Kind)
}

// where is "main a3f21c9d8e7b".
func where(e *api.Activity) string {
	return strings.TrimSpace(render.Safe(e.Branch) + " " + shortSHA(render.Safe(e.CommitSHA)))
}

// deliveryText is "push main a3f21c9d8e7b by ana".
func deliveryText(e *api.Activity) string {
	s := strings.TrimSpace(render.Safe(e.Event) + " " + where(e))
	if e.PushedBy != "" {
		s += " by " + render.Safe(e.PushedBy)
	}
	return s
}

// readText is "3/3 read" for a complete read of the lock files, else its
// status.
func readText(e *api.Activity) string {
	if e.Status == "complete" {
		return fmt.Sprintf("%d/%d read", e.ProjectsRead, e.ProjectsRead+e.ProjectsUnread)
	}
	return render.Safe(e.Status)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// bumpedText is "lodash 4.17.21": the versions HKr read back from the
// lock file after an upgrade, each once, in order. More than one means
// the lock file still has another copy. "" when there are none.
func bumpedText(pkg string, versions []string) string {
	seen := map[string]bool{}
	var vs []string
	for _, v := range versions {
		if !seen[v] {
			seen[v] = true
			vs = append(vs, render.Safe(v))
		}
	}
	if len(vs) == 0 {
		return ""
	}
	return render.Safe(pkg) + " " + strings.Join(vs, ", ")
}
