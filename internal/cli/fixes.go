package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

// fixPoll is how often fix --wait asks how a fix is going.
const fixPoll = 10 * time.Second

func newFindingsFixCmd(a *app) *cobra.Command {
	var dryRun, wait bool
	c := &cobra.Command{
		Use:   "fix <id|url>",
		Short: "Ask HKr for a fix pull request (costs credit)",
		Long: "Ask HKr for a fix: an agent writes a patch in the finding's repository,\n" +
			"proves it against the reproduction, and opens a pull request (or leaves\n" +
			"it for you to open, as your organization prefers). It costs credit: it\n" +
			"says about how much and asks first, or takes --yes. --dry-run only\n" +
			"checks. --wait waits until the fix is done.",
		Example: "  hkr findings fix Fx7Kq2XpAb2m --dry-run\n" +
			"  hkr findings fix Fx7Kq2XpAb2m --wait\n" +
			"  hkr findings fix Fx7Kq2XpAb2m --yes --json",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun && !a.flags.yes && !a.env.StdinIsTerminal {
				return usageError{errors.New("a fix costs credit: pass --yes to ask for it without a terminal (--dry-run says what it would cost)")}
			}
			ctx := cmd.Context()
			cl, org, id, err := a.finding(cmd, args[0])
			if err != nil {
				return err
			}
			if dryRun || !a.flags.yes {
				q, raw, err := cl.QuoteFix(ctx, org, id)
				if err != nil {
					return err
				}
				if dryRun {
					if a.flags.json {
						return a.printJSON(raw)
					}
					a.out("A fix for %s can start: %s.", id, costText(q.EstimateUSD, q.BalanceUSD))
					return nil
				}
				if err := a.confirm(fmt.Sprintf("Ask HKr for a fix for %s? It costs %s.", id, costText(q.EstimateUSD, q.BalanceUSD)),
					"a fix costs credit: pass --yes"); err != nil {
					return err
				}
			}
			fix, raw, err := cl.ProposeFix(ctx, org, id)
			if err != nil {
				return err
			}
			if wait {
				if fix, raw, err = a.waitFix(ctx, cl, org, fix); err != nil {
					return err
				}
			} else if !a.flags.json {
				a.out("Asked for fix %s (%s).", fix.ID, fix.Status)
				a.progress("Check on it: hkr fixes show %s", fix.ID)
				return nil
			}
			return a.fixResult(fix, raw)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "only check that it can start, and what it may cost")
	c.Flags().BoolVar(&wait, "wait", false, "wait until the fix is done")
	return c
}

// costText is "about $10.10 (balance $5000.00)".
func costText(estimate, balance string) string {
	if estimate == "" {
		return fmt.Sprintf("an amount HKr can't estimate (balance $%s)", balance)
	}
	return fmt.Sprintf("about $%s (balance $%s)", estimate, balance)
}

// waitFix polls the fix until it's done, saying each status it passes.
func (a *app) waitFix(ctx context.Context, cl *api.Client, org string, fix *api.Fix) (*api.Fix, []byte, error) {
	sleep := a.env.Sleep
	if sleep == nil {
		sleep = api.Sleep
	}
	a.progress("Fix %s: %s.", fix.ID, fix.Status)
	last := fix.Status
	for {
		if err := sleep(ctx, fixPoll); err != nil {
			return nil, nil, err
		}
		next, raw, err := cl.Fix(ctx, org, fix.ID)
		if err != nil {
			return nil, nil, err
		}
		if next.Done() {
			return next, raw, nil
		}
		if next.Status != last {
			a.progress("Fix %s: %s.", next.ID, next.Status)
			last = next.Status
		}
	}
}

// fixResult reports a fix: its status, and its pull request or what to
// do next.
func (a *app) fixResult(fix *api.Fix, raw []byte) error {
	if a.flags.json {
		return a.printJSON(raw)
	}
	switch {
	case fix.PullRequest != nil:
		a.out("Fix %s is %s: %s", fix.ID, fix.Status, render.Safe(fix.PullRequest.URL))
	case fix.UnverifiedReason != "":
		a.out("Fix %s is %s (%s).", fix.ID, fix.Status, render.Safe(fix.UnverifiedReason))
	default:
		a.out("Fix %s is %s.", fix.ID, fix.Status)
	}
	if fix.PullRequest == nil && (fix.Status == "proven" || fix.Status == "verified") {
		a.progress("Open its pull request: hkr fixes open-pr %s", fix.ID)
	}
	return nil
}

func newFindingsFixesCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "fixes <id|url>",
		Short:   "The fixes asked for a finding, and their pull requests",
		Example: "  hkr findings fixes Fx7Kq2XpAb2m",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.finding(cmd, args[0])
			if err != nil {
				return err
			}
			return printList(cmd.Context(), a, cl, &l, api.FindingPath(org, id)+"/fixes", nil, func(fs []api.Fix) string {
				rows := make([][]string, 0, len(fs))
				for _, f := range fs {
					repo, pr := "", "—"
					if f.Repository != nil {
						repo = f.Repository.FullName
					}
					if f.PullRequest != nil {
						pr = fmt.Sprintf("%s (%s)", f.PullRequest.URL, f.PullRequest.State)
					}
					rows = append(rows, []string{f.ID, f.Status, render.Safe(repo), render.Safe(pr)})
				}
				return render.Table([]string{"ID", "STATUS", "REPOSITORY", "PULL REQUEST"}, rows)
			}, "No fixes asked for "+id+".")
		},
	}
	l.add(c)
	return c
}

func newFixesCmd(a *app) *cobra.Command {
	return group(a, "fixes", "One fix: how it's going, and opening its pull request",
		"  hkr fixes show Fx7Kq2XpAb2m\n  hkr fixes open-pr Fx7Kq2XpAb2m",
		newFixesShowCmd(a), newFixesOpenPRCmd(a))
}

func newFixesShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "show <fix>",
		Short:   "How a fix is going, and its pull request",
		Example: "  hkr fixes show Fx7Kq2XpAb2m --json",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, err := a.inOrg(cmd.Context())
			if err != nil {
				return err
			}
			fix, raw, err := cl.Fix(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			return a.fixResult(fix, raw)
		},
	}
}

func newFixesOpenPRCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "open-pr <fix>",
		Short: "Open the pull request for a fix HKr left for you",
		Long: "Open the pull request for a fix, when your organization has HKr leave\n" +
			"them for a person to open.",
		Example: "  hkr fixes open-pr Fx7Kq2XpAb2m",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, err := a.inOrg(cmd.Context())
			if err != nil {
				return err
			}
			fix, raw, err := cl.OpenPullRequest(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			if fix.PullRequest == nil {
				a.out("Fix %s is %s; HKr is opening its pull request.", fix.ID, fix.Status)
				return nil
			}
			a.out("Opened %s", render.Safe(fix.PullRequest.URL))
			return nil
		},
	}
}
