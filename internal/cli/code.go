package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/chrom-ar/hkr-cli/internal/target"
	"github.com/spf13/cobra"
)

// repoID is the id of the repository arg names: an id, or a full name.
func (a *app) repoID(ctx context.Context, cl *api.Client, org, arg string) (string, error) {
	if target.IsID(arg) {
		return arg, nil
	}
	page, err := api.List[api.Repo](ctx, cl, api.OrgPath(org)+"/repos", nil, true)
	if err != nil {
		return "", err
	}
	for _, r := range page.Items {
		if strings.EqualFold(r.FullName, arg) {
			return r.ID, nil
		}
	}
	return "", usageError{fmt.Errorf("no repository matches %q; see hkr repos list", arg)}
}

func (a *app) inRepo(ctx context.Context, arg string) (*api.Client, string, string, error) {
	cl, org, err := a.inOrg(ctx)
	if err != nil {
		return nil, "", "", err
	}
	id, err := a.repoID(ctx, cl, org, arg)
	return cl, org, id, err
}

// repoText is a repository's settings, as repos show and set print them.
func repoText(r *api.Repo) string {
	var b strings.Builder
	vis := "public"
	if r.Private {
		vis = "private"
	}
	fmt.Fprintf(&b, "%s  %s, %s, %s\n", render.Safe(r.FullName), r.Provider, render.Safe(r.DefaultBranch), vis)
	reviews := "off"
	if r.ReviewOnPush {
		reviews = "on every push"
		if len(r.ReviewBranches) > 0 {
			reviews += " to " + render.Safe(strings.Join(r.ReviewBranches, ", "))
		}
		if len(r.ReviewPaths) > 0 {
			reviews += ", under " + render.Safe(strings.Join(r.ReviewPaths, ", "))
		}
	}
	fmt.Fprintf(&b, "%-10s%s\n", "Reviews", reviews)
	if r.WatchDependencies {
		fix := "fixing none on its own"
		if r.FixLevel != "" && r.FixLevel != "off" {
			fix = "fixing " + r.FixLevel + " on its own"
		}
		fmt.Fprintf(&b, "%-10s%s\n", "Watching", "dependencies, "+fix)
	}
	if r.Archived {
		fmt.Fprintf(&b, "%-10s%s\n", "Archived", "yes")
	}
	fmt.Fprintf(&b, "%-10s%s", "URL", render.Safe(r.URL))
	return b.String()
}

// onOff reads an on|off flag.
func onOff(flag, v string) (bool, error) {
	switch v {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, usageError{fmt.Errorf("invalid %s %q: use on or off", flag, v)}
}

// splitList splits a comma-separated flag; "" is an empty list.
func splitList(v string) []string {
	out := []string{}
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func newReposSetCmd(a *app) *cobra.Command {
	var reviewOnPush, branches, paths, watchDeps, fixLevel string
	var minMinutes int
	c := &cobra.Command{
		Use:   "set <repo>",
		Short: "Change what HKr does with a repository (organization admins, setup token)",
		Long: "Change a repository's settings: reviews on push (to which branches,\n" +
			"under which paths, at most how often), dependency watching, and the\n" +
			"fix level, the lowest severity HKr fixes dependency CVEs at on its own.\n" +
			"Reviews on push and a fix level other than off let HKr spend: they\n" +
			"ask, or take --yes.\n" +
			"Needs a token with setup changes (on by default in hkr login).",
		Example: "  hkr repos set acme/web --review-on-push on --branches main\n" +
			"  hkr repos set acme/web --watch-deps on --fix-level critical --yes",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			body := map[string]any{}
			for flag, key := range map[string]string{"review-on-push": "review_on_push", "watch-deps": "watch_dependencies"} {
				if f.Changed(flag) {
					v, _ := f.GetString(flag)
					on, err := onOff("--"+flag, v)
					if err != nil {
						return err
					}
					body[key] = on
				}
			}
			if f.Changed("branches") {
				body["review_branches"] = splitList(branches)
			}
			if f.Changed("paths") {
				body["review_paths"] = splitList(paths)
			}
			if f.Changed("min-minutes") {
				body["review_min_minutes"] = minMinutes
			}
			if f.Changed("fix-level") {
				body["fix_level"] = fixLevel
			}
			if len(body) == 0 {
				return usageError{errors.New("nothing to change: pass a setting (see hkr repos set --help)")}
			}
			if f.Changed("fix-level") && fixLevel != "off" {
				if !a.flags.yes && !a.env.StdinIsTerminal {
					return usageError{errors.New("a fix level lets HKr spend on its own: pass --yes to set it without a terminal")}
				}
				if err := a.confirm(fmt.Sprintf("Let HKr fix %s and worse dependency CVEs on its own, spending credit?", render.Safe(fixLevel)),
					"a fix level lets HKr spend on its own: pass --yes"); err != nil {
					return err
				}
			}
			if on, _ := body["review_on_push"].(bool); on {
				if !a.flags.yes && !a.env.StdinIsTerminal {
					return usageError{errors.New("each push review spends credit: pass --yes to turn reviews on without a terminal")}
				}
				if err := a.confirm("Review every push? Each review spends credit.", "each push review spends credit: pass --yes"); err != nil {
					return err
				}
			}
			ctx := cmd.Context()
			cl, org, id, err := a.inRepo(ctx, args[0])
			if err != nil {
				return err
			}
			var r api.Repo
			raw, err := cl.Send(ctx, http.MethodPatch, api.OrgPath(org)+"/repos/"+api.Seg(id), nil, body, &r)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s", repoText(&r))
			switch r.PushHook {
			case "insufficient_access":
				a.warn("HKr couldn't add the push webhook: the connection needs more access (GitLab: Maintainer)")
			case "failed":
				a.warn("HKr couldn't add the push webhook; reviews won't start until it can: try again later")
			}
			a.nextStep(ctx, cl, org)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&reviewOnPush, "review-on-push", "", "on or off")
	f.StringVar(&branches, "branches", "", "only pushes to these branches (comma-separated; '' for all)")
	f.StringVar(&paths, "paths", "", "only changes under these paths (comma-separated; '' for all)")
	f.IntVar(&minMinutes, "min-minutes", 0, "at most one review this often, in minutes")
	f.StringVar(&watchDeps, "watch-deps", "", "on or off: watch dependencies for CVEs")
	f.StringVar(&fixLevel, "fix-level", "", "off, critical, high or medium")
	return c
}

// shortSHA is a commit's first 12 characters, as git abbreviates them.
func shortSHA(sha string) string { return sha[:min(len(sha), 12)] }

func reposTable(rs []api.Repo) string {
	rows := make([][]string, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, []string{r.ID, render.Safe(r.FullName), r.Provider, yesNo(r.ReviewOnPush), r.FixLevel})
	}
	return render.Table([]string{"ID", "REPOSITORY", "PROVIDER", "REVIEW ON PUSH", "FIX LEVEL"}, rows)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func newReposCmd(a *app) *cobra.Command {
	list := &cobra.Command{
		Use:     "list",
		Short:   "The connected repositories (organization admins)",
		Example: "  hkr repos list",
		Args:    noArgs,
	}
	var l listFlags
	l.add(list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		cl, org, err := a.inOrg(cmd.Context())
		if err != nil {
			return err
		}
		return printList(cmd.Context(), a, cl, &l, api.OrgPath(org)+"/repos", nil, reposTable,
			"No repositories connected. Connect a git host: hkr repos connect github")
	}
	show := &cobra.Command{
		Use:     "show <repo>",
		Short:   "One repository: what HKr does with it",
		Example: "  hkr repos show acme/web",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inRepo(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var r api.Repo
			raw, err := cl.Get(cmd.Context(), api.OrgPath(org)+"/repos/"+api.Seg(id), nil, &r)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s", repoText(&r))
			return nil
		},
	}
	return group(a, "repos", "Connected repositories (organization admins)",
		"  hkr repos list\n  hkr repos show acme/web\n  hkr repos connect github", list, show, newReposConnectCmd(a), newReposSetCmd(a), newActivityCmd(a, true))
}

func newReviewsCmd(a *app) *cobra.Command {
	var l listFlags
	list := &cobra.Command{
		Use:     "list <repo>",
		Short:   "A repository's push reviews, newest first",
		Example: "  hkr reviews list acme/web",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inRepo(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printList(cmd.Context(), a, cl, &l, api.OrgPath(org)+"/repos/"+api.Seg(id)+"/reviews", nil, func(rs []api.Review) string {
				rows := make([][]string, 0, len(rs))
				for _, r := range rs {
					status := r.Status
					if r.Reason != "" {
						status += " (" + r.Reason + ")"
					}
					rows = append(rows, []string{r.ID, render.Safe(status), render.Safe(r.Branch), shortSHA(r.CommitSHA),
						strconv.Itoa(r.Comments), render.Safe(render.Clip(r.Summary, 60))})
				}
				return render.Table([]string{"ID", "STATUS", "BRANCH", "COMMIT", "COMMENTS", "SUMMARY"}, rows)
			}, "No reviews yet.")
		},
	}
	l.add(list)
	show := &cobra.Command{
		Use:     "show <repo> <review>",
		Short:   "One review: its summary and comments",
		Example: "  hkr reviews show acme/web Rv2Kq2XpAb2m",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inRepo(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var r api.ReviewDetail
			raw, err := cl.Get(cmd.Context(), api.OrgPath(org)+"/repos/"+api.Seg(id)+"/reviews/"+api.Seg(args[1]), nil, &r)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			where := render.Safe(r.Branch)
			if r.PullRequestNumber > 0 {
				where += fmt.Sprintf(", pull request #%d", r.PullRequestNumber)
			}
			a.out("Review %s of %s at %s (%s): %s", r.ID, render.Safe(args[0]), shortSHA(r.CommitSHA), where, r.Status)
			if r.Summary != "" {
				a.out("\n%s", render.SafeText(r.Summary))
			}
			for _, c := range r.Comments {
				a.out("\n%s  %s:%d  %s", c.Severity, render.Safe(c.Path), c.Line, render.Safe(c.Title))
				for _, line := range strings.Split(strings.TrimRight(render.SafeText(c.Body), "\n"), "\n") {
					a.out("%s%s", strings.Repeat(" ", len(c.Severity)+2), line)
				}
			}
			return nil
		},
	}
	return group(a, "reviews", "A repository's push reviews (organization admins)",
		"  hkr reviews list acme/web\n  hkr reviews show acme/web Rv2Kq2XpAb2m", list, show)
}

func newDepsCmd(a *app) *cobra.Command {
	var (
		l            listFlags
		status, repo string
	)
	list := &cobra.Command{
		Use:   "list",
		Short: "Dependency CVEs in your repositories, worst first",
		Long: "Dependency CVEs across the organization's watched repositories. --status\n" +
			"is one or more of open, awaiting_credit (the default: both), resolved\n" +
			"and dismissed.",
		Example: "  hkr deps list\n  hkr deps list --status dismissed --repo acme/web",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, s := range strings.Split(status, ",") {
				if err := oneOf("--status", strings.TrimSpace(s), "open", "awaiting_credit", "resolved", "dismissed"); err != nil {
					return err
				}
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
			return printList(ctx, a, cl, &l, api.OrgPath(org)+"/dependencies", q, func(es []api.Exposure) string {
				rows := make([][]string, 0, len(es))
				for _, e := range es {
					x := e.Advisory.Exploitation
					exploited := ""
					if x.KnownExploited {
						exploited = "yes"
					}
					rows = append(rows, []string{e.ID, e.Advisory.Severity, exploited, percent(x.EPSS), render.Safe(e.PackageName + "@" + e.MatchedVersion),
						render.Safe(e.FixedVersion), render.Safe(e.Repository.FullName), e.Status, render.Safe(e.Advisory.Name())})
				}
				return render.Table([]string{"ID", "SEVERITY", "EXPLOITED", "EPSS", "PACKAGE", "FIXED IN", "REPOSITORY", "STATUS", "ADVISORY"}, rows)
			}, "No dependency CVEs.")
		},
	}
	list.Flags().StringVar(&status, "status", "", "open, awaiting_credit, resolved, dismissed (comma-separated)")
	list.Flags().StringVar(&repo, "repo", "", "only this repository")
	l.add(list)

	show := &cobra.Command{
		Use:     "show <id>",
		Short:   "One dependency CVE: the advisory, the package, the fix",
		Example: "  hkr deps show Ex2Kq2XpAb2m",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, raw, err := a.exposure(cmd.Context(), http.MethodGet, args[0], "")
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s@%s (%s, %s) in %s: %s", render.Safe(e.PackageName), render.Safe(e.MatchedVersion), render.Safe(e.Ecosystem),
				e.Scope, render.Safe(e.Repository.FullName), render.Safe(e.Advisory.Name()))
			line := func(label, v string) {
				if v != "" {
					a.out("%-10s%s", label, v)
				}
			}
			line("Severity", e.Advisory.Severity)
			line("Status", e.Status)
			line("Fixed in", render.Safe(e.FixedVersion))
			line("Summary", render.Safe(e.Advisory.Summary))
			x := e.Advisory.Exploitation
			if x.KnownExploited {
				text := "yes, in CISA's KEV"
				if x.KEVAddedOn != "" {
					text += " since " + day(x.KEVAddedOn)
				}
				if x.KEVDueOn != "" {
					text += "; fix by " + day(x.KEVDueOn)
				}
				if x.Ransomware != nil && *x.Ransomware {
					text += "; used in ransomware campaigns"
				}
				line("Exploited", text)
				line("Action", render.Safe(x.CISARequiredAction))
			}
			line("SSVC", ssvcText(x))
			if x.EPSS != nil {
				text := percent(x.EPSS) + " chance of exploitation in 30 days"
				var more []string
				if x.EPSSPercentile != nil {
					more = append(more, "above "+percent(x.EPSSPercentile)+" of CVEs")
				}
				if x.EPSSOn != "" {
					more = append(more, day(x.EPSSOn))
				}
				if len(more) > 0 {
					text += " (" + strings.Join(more, ", ") + ")"
				}
				line("EPSS", text)
			}
			line("CWE", render.Safe(strings.Join(e.Advisory.CWEIDs, ", ")))
			if e.Fix != nil {
				fix := e.Fix.ID + ", " + e.Fix.Status
				if e.Fix.PullRequest != nil {
					fix += ": " + render.Safe(e.Fix.PullRequest.URL)
				}
				line("Fix", fix)
			}
			if e.MergeUndone {
				line("Note", "a merged upgrade was undone by a later change")
			}
			return nil
		},
	}

	fix := &cobra.Command{
		Use:   "fix <id>",
		Short: "Ask HKr for an upgrade pull request now (costs credit)",
		Example: "  hkr deps fix Ex2Kq2XpAb2m\n" +
			"  hkr deps fix Ex2Kq2XpAb2m --yes",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !a.flags.yes && !a.env.StdinIsTerminal {
				return usageError{errors.New("an upgrade costs credit: pass --yes to ask for it without a terminal")}
			}
			if err := a.confirm("Ask HKr for an upgrade pull request for "+args[0]+"? It costs credit.", "an upgrade costs credit: pass --yes"); err != nil {
				return err
			}
			cl, org, err := a.inOrg(cmd.Context())
			if err != nil {
				return err
			}
			var f api.Fix
			raw, err := cl.Send(cmd.Context(), http.MethodPost, api.OrgPath(org)+"/dependencies/"+api.Seg(args[0])+"/fix", nil, nil, &f)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("Asked for fix %s (%s).", f.ID, f.Status)
			a.progress("Check on it: hkr fixes show %s", f.ID)
			return nil
		},
	}

	change := func(verb, short string) *cobra.Command {
		return &cobra.Command{
			Use:     verb + " <id>",
			Short:   short,
			Example: "  hkr deps " + verb + " Ex2Kq2XpAb2m",
			Args:    argsBetween(1, 1),
			RunE: func(cmd *cobra.Command, args []string) error {
				e, raw, err := a.exposure(cmd.Context(), http.MethodPost, args[0], "/"+verb)
				if err != nil {
					return err
				}
				if a.flags.json {
					return a.printJSON(raw)
				}
				a.out("%s is %s.", e.ID, e.Status)
				return nil
			},
		}
	}
	return group(a, "deps", "Dependency CVEs in your repositories (organization admins)",
		"  hkr deps list\n  hkr deps fix Ex2Kq2XpAb2m",
		list, show, fix,
		change("dismiss", "Say it isn't a risk here; a later match won't reopen it"),
		change("restore", "Undo a dismissal"))
}

// day shows a date (2026-11-10) as "10 Nov 2026".
func day(date string) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return render.Safe(date)
	}
	return d.Format("2 Jan 2006")
}

// percent is 0.912 as "91.2%"; "" for nil.
func percent(p *float64) string {
	switch {
	case p == nil:
		return ""
	case *p > 0 && *p < 0.001:
		return "<0.1%"
	}
	return strconv.FormatFloat(*p*100, 'f', 1, 64) + "%"
}

// ssvcText is CISA's SSVC decision points: "exploitation active,
// automatable, technical impact partial"; "" when CISA said nothing.
func ssvcText(x api.Exploitation) string {
	var parts []string
	if x.SSVCExploitation != "" {
		parts = append(parts, "exploitation "+render.Safe(x.SSVCExploitation))
	}
	if x.SSVCAutomatable != nil {
		if *x.SSVCAutomatable {
			parts = append(parts, "automatable")
		} else {
			parts = append(parts, "not automatable")
		}
	}
	if x.SSVCTechnicalImpact != "" {
		parts = append(parts, "technical impact "+render.Safe(x.SSVCTechnicalImpact))
	}
	return strings.Join(parts, ", ")
}

func (a *app) exposure(ctx context.Context, method, id, action string) (*api.Exposure, []byte, error) {
	cl, org, err := a.inOrg(ctx)
	if err != nil {
		return nil, nil, err
	}
	var e api.Exposure
	raw, err := cl.Send(ctx, method, api.OrgPath(org)+"/dependencies/"+api.Seg(id)+action, nil, nil, &e)
	if err != nil {
		return nil, nil, err
	}
	return &e, raw, nil
}
