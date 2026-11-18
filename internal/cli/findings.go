package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/chrom-ar/hkr-cli/internal/target"
	"github.com/spf13/cobra"
)

func newFindingsCmd(a *app) *cobra.Command {
	return group(a, "findings", "What HKr found, its evidence and fixes",
		"  hkr findings list --severity high\n"+
			"  hkr findings show Fx7Kq2XpAb2m --markdown > finding.md\n"+
			"  hkr findings fix Fx7Kq2XpAb2m --wait",
		newFindingsListCmd(a), newFindingsShowCmd(a), newFindingsEvidenceCmd(a),
		newFindingsFixCmd(a), newFindingsFixesCmd(a),
		newFindingChangeCmd(a, "mark-fixed", "Confirm HKr's suggestion that it's fixed", "is marked fixed"),
		newFindingsAcceptCmd(a),
		newFindingChangeCmd(a, "reopen", "Undo an acceptance: open it again", ""))
}

// severities, least severe first.
var severities = []string{"informational", "low", "medium", "high", "critical"}

// severityQuery reads --severity: one level is a minimum (high: high and
// critical), a comma-separated list is exactly those.
func severityQuery(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	levels := strings.Split(v, ",")
	for i, l := range levels {
		levels[i] = strings.ToLower(strings.TrimSpace(l))
		if !slices.Contains(severities, levels[i]) {
			return "", usageError{fmt.Errorf("unknown severity %q: use critical, high, medium, low or informational", l)}
		}
	}
	if len(levels) == 1 {
		return strings.Join(severities[slices.Index(severities, levels[0]):], ","), nil
	}
	return strings.Join(levels, ","), nil
}

func oneOf(flag, v string, allowed ...string) error {
	if v == "" || slices.Contains(allowed, v) {
		return nil
	}
	last := len(allowed) - 1
	return usageError{fmt.Errorf("unknown %s %q: use %s or %s", flag, v, strings.Join(allowed[:last], ", "), allowed[last])}
}

func newFindingsListCmd(a *app) *cobra.Command {
	var (
		l                                      listFlags
		state, severity, domain, class, source string
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "Findings across your domains, most severe first",
		Long: "Findings across your domains, most severe first. --state open (the\n" +
			"default) is suspected, confirmed, not fixed and partially fixed; fixed\n" +
			"includes refuted. --severity high means high and critical; a list\n" +
			"(high,low) means exactly those.",
		Example: "  hkr findings list\n" +
			"  hkr findings list --severity high --json\n" +
			"  hkr findings list --state accepted --domain Dm2Kq2XpAb2m",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := oneOf("--state", state, "open", "fixed", "accepted", "all"); err != nil {
				return err
			}
			if err := oneOf("--source", source, "scan", "review"); err != nil {
				return err
			}
			sev, err := severityQuery(severity)
			if err != nil {
				return err
			}
			cl, org, err := a.inOrg(cmd.Context())
			if err != nil {
				return err
			}
			q := api.Query{"state": state, "severity": sev, "domain": domain, "class": class, "source": source}
			if state == "open" {
				delete(q, "state") // The default; leaving it out keeps the request what HKr expects.
			}
			empty := "No open findings."
			switch state {
			case "fixed", "accepted":
				empty = "No " + state + " findings."
			case "all":
				empty = "No findings."
			}
			return printList(cmd.Context(), a, cl, &l, api.OrgPath(org)+"/findings", q, findingsTable, empty)
		},
	}
	f := c.Flags()
	f.StringVar(&state, "state", "", "open (default), fixed, accepted or all")
	f.StringVar(&severity, "severity", "", "a minimum (high), or a list (high,low)")
	f.StringVar(&domain, "domain", "", "only this domain (its id)")
	f.StringVar(&class, "class", "", "only this class: idor, sqli, xss-stored, …")
	f.StringVar(&source, "source", "", "scan or review")
	l.add(c)
	return c
}

func findingsTable(fs []api.Finding) string {
	rows := make([][]string, 0, len(fs))
	for _, f := range fs {
		rows = append(rows, []string{f.ID, f.Severity, f.Status, render.Safe(f.Domain.RootDomain), render.Safe(render.Clip(f.Title, 80))})
	}
	return render.Table([]string{"ID", "SEVERITY", "STATUS", "DOMAIN", "TITLE"}, rows)
}

// finding is a signed-in client, the organization, and the finding id
// that ref (an id or a web URL) names.
func (a *app) finding(cmd *cobra.Command, ref string) (*api.Client, string, string, error) {
	cl, _, err := a.client()
	if err != nil {
		return nil, "", "", err
	}
	id, err := target.ParseFinding(ref, cl.Origin())
	if err != nil {
		return nil, "", "", err
	}
	org, err := a.resolver(cl).Org(cmd.Context())
	if err != nil {
		return nil, "", "", err
	}
	a.org = org
	return cl, org, id, nil
}

func newFindingsShowCmd(a *app) *cobra.Command {
	var markdown bool
	c := &cobra.Command{
		Use:   "show <id|url>",
		Short: "One finding in full: reproduction, impact, remediation",
		Long: "One finding in full. --markdown prints HKr's own Markdown for it, the\n" +
			"same as the web app's finding.md, written to hand to an AI.",
		Example: "  hkr findings show Fx7Kq2XpAb2m\n" +
			"  hkr findings show https://app.hkr.chrom.ar/findings/Fx7Kq2XpAb2m\n" +
			"  hkr findings show Fx7Kq2XpAb2m --markdown > finding.md",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.finding(cmd, args[0])
			if err != nil {
				return err
			}
			if markdown {
				md, err := cl.Markdown(cmd.Context(), api.FindingPath(org, id), nil)
				if err != nil {
					return err
				}
				fmt.Fprintln(a.env.Stdout, strings.TrimRight(render.SafeText(md), "\n"))
				return nil
			}
			f, raw, err := cl.Finding(cmd.Context(), org, id)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s", a.findingText(f))
			return nil
		},
	}
	c.Flags().BoolVar(&markdown, "markdown", false, "HKr's Markdown for the finding, for an AI")
	return c
}

func (a *app) findingText(f *api.Finding) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%-10s%s\n", label, render.Safe(value))
		}
	}
	fmt.Fprintf(&b, "%s  %s\n", render.Safe(f.Ref), render.Safe(f.Title))
	sev := f.Severity
	switch {
	case f.CVSSScore != nil && f.CVSSVector != "":
		sev += fmt.Sprintf(" (CVSS %.1f, %s)", *f.CVSSScore, f.CVSSVector)
	case f.CVSSScore != nil:
		sev += fmt.Sprintf(" (CVSS %.1f)", *f.CVSSScore)
	}
	line("Severity", sev)
	status := f.Status
	if f.Accepted != nil {
		status += ": " + f.Accepted.Reason
		if f.Accepted.By != nil {
			status += " (" + f.Accepted.By.FullName() + ")"
		}
	}
	line("Status", status)
	line("Class", f.Class)
	line("Domain", fmt.Sprintf("%s (%s)", f.Domain.Name, f.Domain.RootDomain))
	line("Target", f.Target)
	line("Found", render.Time(f.FirstSeenAt, a.env.Location, "2 Jan 2006 15:04"))
	line("URL", f.URL)
	if s := f.SuggestedFixed; s != nil {
		fmt.Fprintf(&b, "\nHKr thinks this is fixed: %s\n", render.Safe(s.Reason))
		if s.URL != "" {
			fmt.Fprintf(&b, "  %s\n", render.Safe(s.URL))
		}
		fmt.Fprintf(&b, "  Confirm with: hkr findings mark-fixed %s\n", f.ID)
	}
	for _, s := range []struct{ title, text string }{
		{"AFFECTED", f.Affected},
		{"SUMMARY", f.Summary},
		{"SEVERITY JUSTIFICATION", f.SeverityJustification},
		{"REPRODUCTION", f.Reproduction},
		{"IMPACT", f.Impact},
		{"REMEDIATION", f.Remediation},
	} {
		if strings.TrimSpace(s.text) != "" {
			fmt.Fprintf(&b, "\n%s\n%s\n", s.title, strings.TrimRight(render.SafeText(s.text), "\n"))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// newFindingChangeCmd is a change that takes nothing but the finding:
// mark-fixed, reopen.
func newFindingChangeCmd(a *app, action, short, done string) *cobra.Command {
	return &cobra.Command{
		Use:     action + " <id|url>",
		Short:   short,
		Example: "  hkr findings " + action + " Fx7Kq2XpAb2m",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.finding(cmd, args[0])
			if err != nil {
				return err
			}
			f, raw, err := cl.ChangeFinding(cmd.Context(), org, id, action, nil)
			if err != nil {
				return err
			}
			return a.changed(f, raw, done)
		},
	}
}

// changed reports a finding after a change: "F-01 is accepted.", or, with
// no words, its status ("open again (confirmed)").
func (a *app) changed(f *api.Finding, raw []byte, done string) error {
	if a.flags.json {
		return a.printJSON(raw)
	}
	if done == "" {
		done = "is open again (" + f.Status + ")"
	}
	a.out("%s %s.", render.Safe(cmpOr(f.Ref, f.ID)), done)
	return nil
}

func newFindingsAcceptCmd(a *app) *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "accept <id|url> --reason <why>",
		Short: "Accept the risk: the organization lives with it",
		Long: "Accept a finding's risk, saying why: it stops counting as open. The\n" +
			"reason is kept with it, and hkr findings reopen undoes it.",
		Example: "  hkr findings accept Fx7Kq2XpAb2m --reason 'Only reachable from the office VPN'",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return usageError{errors.New("accept needs --reason: why the organization lives with this risk")}
			}
			cl, org, id, err := a.finding(cmd, args[0])
			if err != nil {
				return err
			}
			f, raw, err := cl.ChangeFinding(cmd.Context(), org, id, "accept", map[string]string{"reason": reason})
			if err != nil {
				return err
			}
			return a.changed(f, raw, "is accepted")
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the organization lives with this risk (required)")
	return c
}
