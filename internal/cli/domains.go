package cli

import (
	"bufio"
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

func newDomainsCmd(a *app) *cobra.Command {
	return group(a, "domains", "The domains HKr tests, and setting one up",
		"  hkr domains list\n"+
			"  hkr domains create shop.example.com\n"+
			"  hkr domains verify shop.example.com",
		newDomainsListCmd(a), newDomainsShowCmd(a), newDomainsMapCmd(a), newDomainsExportCmd(a),
		newDomainsCreateCmd(a), newDomainsVerifyCmd(a), newDomainsAttestCmd(a), newDomainsLimitsCmd(a))
}

// domainID is the id of the domain arg names: an id as is, else the one
// whose root domain or name matches (exactly, ignoring case, or the only
// partial match).
func (a *app) domainID(ctx context.Context, cl *api.Client, org, arg string) (string, error) {
	if target.IsID(arg) {
		return arg, nil
	}
	page, err := api.List[api.Domain](ctx, cl, api.OrgPath(org)+"/domains", api.Query{"q": arg}, false)
	if err != nil {
		return "", err
	}
	for _, d := range page.Items {
		if strings.EqualFold(d.RootDomain, arg) || strings.EqualFold(d.Name, arg) {
			return d.ID, nil
		}
	}
	switch len(page.Items) {
	case 0:
		return "", usageError{fmt.Errorf("no domain matches %q; see hkr domains list", arg)}
	case 1:
		return page.Items[0].ID, nil
	}
	names := make([]string, 0, len(page.Items))
	for _, d := range page.Items {
		names = append(names, d.RootDomain)
	}
	return "", usageError{fmt.Errorf("%q matches %d domains: %s", arg, len(names), strings.Join(names, ", "))}
}

// inDomain is a signed-in client, the organization and the domain's id.
func (a *app) inDomain(ctx context.Context, arg string) (*api.Client, string, string, error) {
	cl, org, err := a.inOrg(ctx)
	if err != nil {
		return nil, "", "", err
	}
	id, err := a.domainID(ctx, cl, org, arg)
	return cl, org, id, err
}

// openText is "1 high, 1 low", or "—".
func openText(counts map[string]int) string {
	var parts []string
	for i := len(severities) - 1; i >= 0; i-- {
		if n := counts[severities[i]]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, severities[i]))
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func newDomainsListCmd(a *app) *cobra.Command {
	var (
		l        listFlags
		archived bool
	)
	c := &cobra.Command{
		Use:     "list",
		Short:   "Your organization's domains, newest first",
		Example: "  hkr domains list\n  hkr domains list --archived --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, org, err := a.inOrg(cmd.Context())
			if err != nil {
				return err
			}
			q := api.Query{}
			if archived {
				q["archived"] = "true"
			}
			empty := "No domains yet. Add one: hkr domains create <root-domain>"
			if archived {
				empty = "No archived domains."
			}
			return printList(cmd.Context(), a, cl, &l, api.OrgPath(org)+"/domains", q, func(ds []api.Domain) string {
				rows := make([][]string, 0, len(ds))
				for _, d := range ds {
					last := "—"
					if d.LastScan != nil {
						last = fmt.Sprintf("%d %s", d.LastScan.Number, d.LastScan.Status)
					}
					rows = append(rows, []string{d.ID, render.Safe(d.Name), render.Safe(d.RootDomain), d.Status, openText(d.OpenFindings), last})
				}
				return render.Table([]string{"ID", "NAME", "ROOT DOMAIN", "STATUS", "OPEN", "LAST SCAN"}, rows)
			}, empty)
		},
	}
	c.Flags().BoolVar(&archived, "archived", false, "the archived domains instead")
	l.add(c)
	return c
}

func newDomainsShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "show <domain>",
		Short:   "One domain: setup, limits, open findings, last scan",
		Example: "  hkr domains show shop.example.com",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inDomain(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			d, raw, err := cl.Domain(cmd.Context(), org, id)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			var b strings.Builder
			line := func(label, value string) {
				if value != "" {
					fmt.Fprintf(&b, "%-10s%s\n", label, render.Safe(value))
				}
			}
			fmt.Fprintf(&b, "%s  %s\n", render.Safe(d.Name), render.Safe(d.RootDomain))
			var setup []string
			if !d.Verified {
				setup = append(setup, "not verified")
			}
			if !d.Attested {
				setup = append(setup, "not attested")
			}
			status := d.Status
			if len(setup) > 0 {
				status += ": " + strings.Join(setup, ", ")
			}
			if d.Archived {
				status += " (archived)"
			}
			line("Status", status)
			line("Scope", d.ScopeBreadth)
			line("Open", openText(d.OpenFindings))
			line("Limits", limitsText(d))
			if d.LastScan != nil {
				line("Last scan", fmt.Sprintf("%d, %s %s", d.LastScan.Number, d.LastScan.Status,
					render.Time(d.LastScan.FinishedAt, a.env.Location, "2 Jan 2006 15:04")))
			}
			line("URL", d.URL)
			a.out("%s", strings.TrimRight(b.String(), "\n"))
			a.nextStep(cmd.Context(), cl, org)
			return nil
		},
	}
}

// limitsText is a domain's default scan limits.
func limitsText(d *api.Domain) string {
	var parts []string
	if d.PresetKey != "" {
		parts = append(parts, d.PresetKey)
	}
	if d.MaxSpendUSD != "" {
		parts = append(parts, "up to $"+d.MaxSpendUSD)
	}
	if d.MaxDurationSeconds > 0 {
		parts = append(parts, "up to "+durationText(secondsDuration(d.MaxDurationSeconds)))
	}
	if len(parts) == 0 {
		return "the preset's"
	}
	return strings.Join(parts, ", ")
}

func secondsDuration(s int) time.Duration { return time.Duration(s) * time.Second }

// durationText is 4h, 90m or 1h30m.
func durationText(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

func newDomainsMapCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "map <domain>",
		Short:   "What HKr has mapped of a domain: hosts, subdomains, addresses",
		Example: "  hkr domains map shop.example.com",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inDomain(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printList(cmd.Context(), a, cl, &l, api.DomainPath(org, id)+"/targets", nil, func(ts []api.Target) string {
				rows := make([][]string, 0, len(ts))
				for _, t := range ts {
					scope := "out"
					if t.InScope {
						scope = "in"
					}
					rows = append(rows, []string{render.Safe(t.Value), t.Kind, scope, t.DiscoveredBy})
				}
				return render.Table([]string{"VALUE", "KIND", "SCOPE", "FOUND BY"}, rows)
			}, "Nothing mapped yet.")
		},
	}
	l.add(c)
	return c
}

func newDomainsExportCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "export <domain>",
		Short: "The domain's open findings as one Markdown report",
		Example: "  hkr domains export shop.example.com > findings.md\n" +
			"  hkr domains export shop.example.com --lang es",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inDomain(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			md, err := cl.Markdown(cmd.Context(), api.DomainPath(org, id)+"/findings.md", nil)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.env.Stdout, strings.TrimRight(render.SafeText(md), "\n"))
			return nil
		},
	}
}

func newDomainsCreateCmd(a *app) *cobra.Command {
	var name, scope string
	c := &cobra.Command{
		Use:   "create <root-domain>",
		Short: "Add a domain for HKr to test",
		Long: "Add a domain. Before HKr scans it, prove you control it (hkr domains\n" +
			"verify) and state you're authorized to test it (hkr domains attest).\n" +
			"--scope domain (the default) includes its subdomains; host is only it.",
		Example: "  hkr domains create shop.example.com --name Shop",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("--scope", scope, "domain", "host"); err != nil {
				return err
			}
			cl, org, err := a.inOrg(cmd.Context())
			if err != nil {
				return err
			}
			body := map[string]string{}
			for k, v := range map[string]string{"root_domain": args[0], "name": name, "scope_breadth": scope} {
				if strings.TrimSpace(v) != "" {
					body[k] = strings.TrimSpace(v)
				}
			}
			d, raw, err := cl.SendDomain(cmd.Context(), http.MethodPost, api.OrgPath(org)+"/domains", body)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("Created %s (%s): %s", render.Safe(d.Name), render.Safe(d.RootDomain), d.ID)
			a.nextStep(cmd.Context(), cl, org)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "what to call it (default: the root domain)")
	c.Flags().StringVar(&scope, "scope", "", "domain (with subdomains, the default) or host")
	return c
}

func newDomainsVerifyCmd(a *app) *cobra.Command {
	var (
		check           bool
		method, address string
	)
	c := &cobra.Command{
		Use:   "verify <domain>",
		Short: "Prove your organization controls a domain",
		Long: "Prove your organization controls a domain, which also covers its\n" +
			"subdomains. With nothing open, it starts a challenge and says what to\n" +
			"publish (a DNS TXT record by default); with one open, it checks it now.\n" +
			"--check only checks. A check exits 1 until it passes, so a script can wait:\n" +
			"  until hkr domains verify shop.example.com --check; do sleep 60; done\n" +
			"--method http_file and email are for organization admins.",
		Example: "  hkr domains verify shop.example.com\n" +
			"  hkr domains verify shop.example.com --check\n" +
			"  hkr domains verify shop.example.com --method email --address security",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("--method", method, "dns_txt", "http_file", "email"); err != nil {
				return err
			}
			if address != "" && method != "email" {
				return usageError{errors.New("--address goes with --method email")}
			}
			ctx := cmd.Context()
			cl, org, id, err := a.inDomain(ctx, args[0])
			if err != nil {
				return err
			}
			path := api.DomainPath(org, id) + "/verification"
			var v *api.Verification
			var raw []byte
			checked := check
			switch {
			case check:
				v, raw, err = cl.Verification(ctx, http.MethodPost, path+"/check", nil)
			case method != "":
				v, raw, err = a.startVerification(ctx, cl, path, method, address)
			default:
				if v, raw, err = cl.Verification(ctx, http.MethodGet, path, nil); err != nil {
					return err
				}
				switch {
				case v.Verified:
				case v.Challenge != nil && v.Challenge.Status == "pending" && v.Challenge.Method != "email":
					checked = true
					v, raw, err = cl.Verification(ctx, http.MethodPost, path+"/check", nil)
				case v.Challenge != nil && v.Challenge.Status == "pending":
				default:
					v, raw, err = a.startVerification(ctx, cl, path, "dns_txt", "")
				}
			}
			if err != nil {
				return err
			}
			if a.flags.json {
				if err := a.printJSON(raw); err != nil {
					return err
				}
			} else {
				a.verificationText(v, args[0])
			}
			if checked && !v.Verified {
				return silentError{ExitError} // So a script can wait for it.
			}
			a.nextStep(ctx, cl, org)
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "only check the open challenge now")
	c.Flags().StringVar(&method, "method", "", "start a challenge: dns_txt (default), http_file or email (admins)")
	c.Flags().StringVar(&address, "address", "", "with --method email: the role address (security, admin, hostmaster, …)")
	return c
}

func (a *app) startVerification(ctx context.Context, cl *api.Client, path, method, address string) (*api.Verification, []byte, error) {
	body := map[string]string{"method": method}
	if address != "" {
		body["address"] = address
	}
	return cl.Verification(ctx, http.MethodPost, path, body)
}

// verificationText says where verification stands and what to do.
func (a *app) verificationText(v *api.Verification, arg string) {
	ch := v.Challenge
	if v.Verified {
		domain := arg
		if ch != nil {
			domain = ch.Domain
		}
		a.out("%s is verified.", render.Safe(domain))
		return
	}
	if ch == nil {
		a.out("Not verified, and no challenge is open. Start one: hkr domains verify %s", render.Safe(arg))
		return
	}
	if ch.LastCheckedAt != "" || ch.LastError != "" {
		reason := ""
		if ch.LastError != "" {
			reason = " (" + render.Safe(ch.LastError) + ")"
		}
		a.out("Not verified yet%s. HKr checks every minute too.", reason)
	}
	switch {
	case ch.Record != nil:
		if ch.LastCheckedAt == "" {
			a.out("Add this DNS record to prove you control %s:", render.Safe(ch.Domain))
		}
		a.out("  %s  %s  %s", render.Safe(ch.Record.Type), render.Safe(ch.Record.Name), render.Safe(ch.Record.Value))
	case ch.File != nil:
		a.out("Serve this at %s:", render.Safe(ch.File.URL))
		a.out("  %s", render.Safe(ch.File.Body))
	case ch.SentTo != "":
		a.out("HKr emailed a link to %s; following it verifies %s.", render.Safe(ch.SentTo), render.Safe(ch.Domain))
		return
	}
	if ch.ExpiresAt != "" {
		a.out("It expires %s. Then: hkr domains verify %s --check", render.Time(ch.ExpiresAt, a.env.Location, "2 Jan 2006 15:04"), render.Safe(arg))
	}
}

func newDomainsAttestCmd(a *app) *cobra.Command {
	var (
		name     string
		accepted bool
	)
	c := &cobra.Command{
		Use:   "attest <domain> --name <full name>",
		Short: "State, in your name, that you're authorized to test a domain",
		Long: "Sign HKr's statement that you have the owner's authorization to test a\n" +
			"domain. It's recorded with your name, once, and can't be changed. At a\n" +
			"terminal it shows the statement and asks you to type the domain; without\n" +
			"one, it needs --statement-accepted (--yes isn't enough).",
		Example: "  hkr domains attest shop.example.com --name 'Ada Lovelace'",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(strings.TrimSpace(name)) < 2 {
				return usageError{errors.New("attest needs --name: your full name, as it will be recorded")}
			}
			ctx := cmd.Context()
			cl, org, id, err := a.inDomain(ctx, args[0])
			if err != nil {
				return err
			}
			at, raw, err := cl.Attestation(ctx, org, id, nil)
			if err != nil {
				return err
			}
			if at.Attested {
				if a.flags.json {
					return a.printJSON(raw)
				}
				a.out("Already attested by %s on %s.", render.Safe(at.AttestedByName), render.Time(at.AttestedAt, a.env.Location, "2 Jan 2006"))
				return nil
			}
			fmt.Fprintf(a.env.Stderr, "You are about to sign, as %s:\n\n  %s\n\n", render.Safe(name), render.Safe(at.Statement))
			if !accepted {
				if !a.env.StdinIsTerminal {
					return usageError{errors.New("attesting signs that statement in your name: pass --statement-accepted to sign it without a terminal")}
				}
				fmt.Fprintf(a.env.Stderr, "Type %s to sign it: ", args[0])
				line, _ := bufio.NewReader(a.env.Stdin).ReadString('\n')
				if strings.TrimSpace(line) != args[0] {
					fmt.Fprintln(a.env.Stderr, "Not signed.")
					return silentError{ExitError}
				}
			}
			at, raw, err = cl.Attestation(ctx, org, id, map[string]string{"name": strings.TrimSpace(name), "statement": at.Statement})
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("Attested %s as %s.", render.Safe(args[0]), render.Safe(at.AttestedByName))
			a.nextStep(ctx, cl, org)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "your full name, as it will be recorded (required)")
	c.Flags().BoolVar(&accepted, "statement-accepted", false, "sign without a terminal: you have read the statement and it is true")
	return c
}

// limitsBody is the scan limits flags as the API takes them; empty flags
// are left out.
func limitsBody(preset, spend, duration string) (map[string]any, error) {
	body := map[string]any{}
	if preset != "" {
		body["preset_key"] = preset
	}
	if spend != "" {
		if _, err := strconv.ParseFloat(spend, 64); err != nil {
			return nil, usageError{fmt.Errorf("invalid --max-spend %q: an amount in USD, like 60 or 12.50", spend)}
		}
		body["max_spend_usd"] = spend
	}
	if duration != "" {
		d, err := time.ParseDuration(duration)
		if err != nil || d < time.Minute {
			return nil, usageError{fmt.Errorf("invalid --max-duration %q: like 4h, 90m or 1h30m", duration)}
		}
		body["max_duration_seconds"] = int(d.Seconds())
	}
	return body, nil
}

// addLimitsFlags adds --preset, --max-spend and --max-duration.
func addLimitsFlags(c *cobra.Command, preset, spend, duration *string) {
	c.Flags().StringVar(preset, "preset", "", "first_look, confidence_check, deep_run or until_critical")
	c.Flags().StringVar(spend, "max-spend", "", "the most a scan may spend, in USD")
	c.Flags().StringVar(duration, "max-duration", "", "a time limit, like 4h or 90m (none by default)")
}

func newDomainsLimitsCmd(a *app) *cobra.Command {
	var preset, spend, duration string
	c := &cobra.Command{
		Use:   "limits <domain>",
		Short: "Show, or set, the default limits of a domain's scans",
		Long: "Show, or set, what a domain's scans may spend. A scan is bounded by\n" +
			"money: it runs up to --max-spend (USD), or stops earlier by its preset:\n" +
			"  first_look        on a confirmed high\n" +
			"  confidence_check  at the first medium it confirms\n" +
			"  deep_run          to its ceiling, unless highs come in a burst\n" +
			"  until_critical    on its count of highs; with no ceiling, the\n" +
			"                    organization's balance is the limit\n" +
			"--max-duration (like 4h or 90m) adds a time limit; there's none by\n" +
			"default.",
		Example: "  hkr domains limits shop.example.com\n" +
			"  hkr domains limits shop.example.com --preset deep_run --max-spend 60",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := limitsBody(preset, spend, duration)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cl, org, id, err := a.inDomain(ctx, args[0])
			if err != nil {
				return err
			}
			var (
				d   *api.Domain
				raw []byte
			)
			if len(body) == 0 {
				d, raw, err = cl.Domain(ctx, org, id)
			} else {
				d, raw, err = cl.SendDomain(ctx, http.MethodPatch, api.DomainPath(org, id), body)
			}
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			text := limitsText(d)
			if text == "the preset's" {
				text = "the preset's limits"
			}
			a.out("Scans of %s: %s.", render.Safe(d.RootDomain), text)
			return nil
		},
	}
	addLimitsFlags(c, &preset, &spend, &duration)
	return c
}
