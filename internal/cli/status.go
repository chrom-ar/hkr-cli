package cli

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newStatusCmd(a *app) *cobra.Command {
	var (
		domain string
		check  bool
	)
	c := &cobra.Command{
		Use:   "status",
		Short: "What's left to set up, and the next step",
		Long: "HKr's setup checklist for the organization: each step, done or not, and\n" +
			"the command for the next one. Optional steps (a git host, links,\n" +
			"schedules) make fixes and recurring scans possible. --check exits 1\n" +
			"while a required step is left, for scripts and agents that loop.",
		Example: "  hkr status\n  hkr status --domain shop.example.com\n  hkr status --check --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, org, err := a.inOrg(ctx)
			if err != nil {
				return err
			}
			id := ""
			if domain != "" {
				if id, err = a.domainID(ctx, cl, org, domain); err != nil {
					return err
				}
			}
			s, raw, err := cl.Setup(ctx, org, id)
			if err != nil {
				return err
			}
			if a.flags.json {
				if err := a.printJSON(raw); err != nil {
					return err
				}
			} else {
				a.out("%s", statusText(s, org))
				if next := nextLine(s, org); next != "" {
					fmt.Fprintln(a.env.Stderr, "\n"+next)
				}
			}
			if check && !s.Complete {
				return silentError{ExitError}
			}
			return nil
		},
	}
	c.Flags().StringVar(&domain, "domain", "", "only this domain's steps")
	c.Flags().BoolVar(&check, "check", false, "exit 1 while a required step is left")
	return c
}

func statusText(s *api.Setup, org string) string {
	done, required := 0, 0
	for _, st := range s.Steps {
		if !st.Optional {
			required++
			if st.Done {
				done++
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d of %d done\n\n", render.Safe(org), done, required)
	for _, st := range s.Steps {
		mark := "✗"
		switch {
		case st.Done:
			mark = "✓"
		case len(st.BlockedBy) > 0:
			mark = "·"
		}
		title, _ := stepCommand(st)
		line := fmt.Sprintf("  %s %s%s%s", mark, title, strings.Repeat(" ", max(2, 30-utf8.RuneCountInString(title))), stepDetail(st))
		if !st.Done && !st.CanAct {
			line += "  ask an admin of " + render.Safe(org)
		}
		if st.Optional {
			line += "  optional"
		}
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// nextStep says, on stderr, the checklist's next step after a setup
// command. It's a hint: any failure to get it says nothing.
func (a *app) nextStep(ctx context.Context, cl *api.Client, org string) {
	if a.flags.quiet || a.flags.json {
		return
	}
	s, _, err := cl.Setup(ctx, org, "")
	if err != nil {
		return
	}
	if next := nextLine(s, org); next != "" {
		fmt.Fprintln(a.env.Stderr, next)
	}
}
