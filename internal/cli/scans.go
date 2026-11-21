package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newScansCmd(a *app) *cobra.Command {
	return group(a, "scans", "A domain's scans: start, watch, steer, stop",
		"  hkr scans start shop.example.com\n"+
			"  hkr scans show shop.example.com\n"+
			"  hkr scans steer shop.example.com 'Look at the admin panel next.'",
		newScansListCmd(a), newScansShowCmd(a), newScansStartCmd(a), newScansFollowCmd(a), newScansSteerCmd(a),
		newScanVerbCmd(a, "wind-down", "Ask a scan to finish what it's on and write it up", false,
			"is winding down: it finishes what it's on and writes it up"),
		newScanVerbCmd(a, "stop", "Stop a scan now; work in progress is lost", true, "is stopping"),
		newScanVerbCmd(a, "resume", "Pick a paused scan back up (it spends credit again)", true, "is starting again"))
}

// scanRef is a domain and a scan number (0: the latest), from
// <domain> [<number>].
type scanRef struct {
	cl          *api.Client
	org, domain string
	arg         string // The domain as typed, for messages.
	number      int
}

func (r scanRef) name() string {
	if r.number == 0 {
		return "the latest scan of " + r.arg
	}
	return fmt.Sprintf("scan %d of %s", r.number, r.arg)
}

func parseNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, usageError{fmt.Errorf("scan number %q: expected a number, like 3", s)}
	}
	return n, nil
}

func (a *app) scanRef(ctx context.Context, domainArg, number string) (scanRef, error) {
	r := scanRef{arg: domainArg}
	if number != "" {
		n, err := parseNumber(number)
		if err != nil {
			return r, err
		}
		r.number = n
	}
	a.domainArg = domainArg
	cl, org, id, err := a.inDomain(ctx, domainArg)
	r.cl, r.org, r.domain = cl, org, id
	return r, err
}

// resolveLatest fixes "latest" to its number, so a verb acts on the scan
// that was latest when asked.
func (r *scanRef) resolveLatest(ctx context.Context) error {
	if r.number > 0 {
		return nil
	}
	s, _, err := r.cl.Scan(ctx, r.org, r.domain, 0)
	if err != nil {
		return err
	}
	r.number = s.Number
	return nil
}

func newScansListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list <domain>",
		Short:   "A domain's scans, newest first",
		Example: "  hkr scans list shop.example.com",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inDomain(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printList(cmd.Context(), a, cl, &l, api.ScansPath(org, id), nil, func(ss []api.Scan) string {
				rows := make([][]string, 0, len(ss))
				for _, s := range ss {
					rows = append(rows, []string{strconv.Itoa(s.Number), a.scanStatus(&s), s.PresetKey, "$" + s.CostUSD,
						render.Time(s.StartedAt, a.env.Location, "2 Jan 2006 15:04"),
						fmt.Sprintf("%d new, %d seen", s.Findings.New, s.Findings.Seen)})
				}
				return render.Table([]string{"SCAN", "STATUS", "PRESET", "COST", "STARTED", "FINDINGS"}, rows)
			}, "No scans yet. Start one: hkr scans start "+render.Safe(args[0]))
		},
	}
	l.add(c)
	return c
}

// scanStatus is the status, with what's been asked of it.
func (a *app) scanStatus(s *api.Scan) string {
	switch {
	case s.Stopping:
		return s.Status + ", stopping"
	case s.WindingDown:
		return s.Status + ", winding down"
	case s.PausedReason == "no_progress":
		return s.Status + " (no progress in 6 hours)"
	case s.PausedReason != "":
		return s.Status + " (" + render.Safe(s.PausedReason) + ")"
	}
	return s.Status
}

func newScansShowCmd(a *app) *cobra.Command {
	var detail string
	c := &cobra.Command{
		Use:   "show <domain> [<number>]",
		Short: "One scan and its timeline (the latest by default)",
		Long: "One scan: status, limits, cost, findings, and its timeline. --detail\n" +
			"key shows only the milestones; all adds the noise.",
		Example: "  hkr scans show shop.example.com\n  hkr scans show shop.example.com 3 --detail all",
		Args:    argsBetween(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("--detail", detail, "key", "normal", "all"); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := a.scanRef(ctx, args[0], argAt(args, 1))
			if err != nil {
				return err
			}
			s, raw, err := r.cl.Scan(ctx, r.org, r.domain, r.number)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			steps, _, err := r.cl.Steps(ctx, r.org, r.domain, s.Number, "")
			if err != nil {
				return err
			}
			a.out("%s", a.scanText(s, r.arg, steps, detail))
			return nil
		},
	}
	c.Flags().StringVar(&detail, "detail", "", "key, normal (the default: no noise) or all")
	return c
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func (a *app) scanText(s *api.Scan, domain string, steps []api.Step, detail string) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%-10s%s\n", label, value)
		}
	}
	fmt.Fprintf(&b, "Scan %d of %s: %s\n", s.Number, render.Safe(domain), a.scanStatus(s))
	limits := s.PresetKey
	if s.MaxSpendUSD != "" {
		limits += ", up to $" + s.MaxSpendUSD
	} else {
		limits += ", no spending ceiling"
	}
	if s.MaxDurationSeconds > 0 {
		limits += ", up to " + durationText(secondsDuration(s.MaxDurationSeconds))
	} else {
		limits += ", no time limit"
	}
	line("Limits", strings.TrimPrefix(limits, ", "))
	line("Cost", "$"+s.CostUSD)
	line("Started", render.Time(s.StartedAt, a.env.Location, "2 Jan 2006 15:04"))
	line("Finished", render.Time(s.FinishedAt, a.env.Location, "2 Jan 2006 15:04"))
	if s.WorkingHoursEndAt != "" && s.FinishedAt == "" {
		line("Deadline", render.Time(s.WorkingHoursEndAt, a.env.Location, "2 Jan 2006 15:04")+", the end of its working hours")
	}
	if s.Findings.New+s.Findings.Seen > 0 {
		line("Findings", fmt.Sprintf("%d new, %d seen", s.Findings.New, s.Findings.Seen))
	}
	line("Asked", render.Safe(s.Instruction))
	if s.Completion != "" {
		line("Ended", upperFirst(completionText(s.Completion))+".")
	}
	line("Error", render.Safe(s.ErrorText))
	line("URL", render.Safe(s.URL))
	var shown []string
	for _, st := range steps {
		if detail != "all" && (st.Importance == "noise" || (detail == "key" && st.Importance != "key")) {
			continue
		}
		shown = append(shown, a.stepLine(st))
	}
	if len(shown) > 0 {
		fmt.Fprintf(&b, "\nTIMELINE\n%s\n", strings.Join(shown, "\n"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// stepLine is "12:00  Mapped the orders API (api.shop.example.com)".
func (a *app) stepLine(st api.Step) string {
	text := render.Safe(st.Summary)
	if st.Target != "" {
		text += " (" + render.Safe(st.Target) + ")"
	}
	return render.Time(st.At, a.env.Location, "15:04") + "  " + text
}

func newScansStartCmd(a *app) *cobra.Command {
	var (
		dryRun, follow                               bool
		instruction, preset, spend, duration, failOn string
	)
	c := &cobra.Command{
		Use:   "start <domain>",
		Short: "Start a scan (costs credit)",
		Long: "Start a scan of a domain, within its limits (hkr domains limits);\n" +
			"--preset, --max-spend and --max-duration change them for this scan only.\n" +
			"It costs credit: it says where the scan stops and asks first, or takes\n" +
			"--yes. --dry-run only checks that it can start. --instruction says\n" +
			"what to look at first.",
		Example: "  hkr scans start shop.example.com --dry-run\n" +
			"  hkr scans start shop.example.com --instruction 'Orders API first'\n" +
			"  hkr scans start shop.example.com --yes --json",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun && !a.flags.yes && !a.env.StdinIsTerminal {
				return usageError{errors.New("a scan costs credit: pass --yes to start it without a terminal (--dry-run says what it would cost)")}
			}
			if failOn != "" && !follow {
				return usageError{errors.New("--fail-on goes with --follow")}
			}
			if _, err := severityQuery(failOn); err != nil {
				return err
			}
			limits, err := limitsBody(preset, spend, duration)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			a.domainArg = args[0]
			cl, org, id, err := a.inDomain(ctx, args[0])
			if err != nil {
				return err
			}
			if dryRun || !a.flags.yes {
				q, raw, err := cl.QuoteScan(ctx, org, id, limits)
				if err != nil {
					return err
				}
				text := quoteText(q, preset)
				if q.PartiallyFunded {
					text += ". Your balance doesn't reach that: it pauses when credit runs out, keeping what it found"
				}
				if dryRun {
					if a.flags.json {
						return a.printJSON(raw)
					}
					a.out("A scan of %s can start: %s.", render.Safe(args[0]), text)
					return nil
				}
				if err := a.confirm(fmt.Sprintf("Start a scan of %s? %s.", render.Safe(args[0]), upperFirst(text)), "a scan costs credit: pass --yes"); err != nil {
					return err
				}
			}
			body := limits
			if strings.TrimSpace(instruction) != "" {
				body["instruction"] = strings.TrimSpace(instruction)
			}
			s, raw, err := cl.StartScan(ctx, org, id, body)
			if err != nil {
				return err
			}
			if follow {
				a.progress("Started scan %d of %s.", s.Number, render.Safe(args[0]))
				return a.followScan(ctx, scanRef{cl: cl, org: org, domain: id, arg: args[0], number: s.Number}, failOn, "")
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("Started scan %d of %s (%s).", s.Number, render.Safe(args[0]), s.Status)
			a.progress("Watch it: hkr scans follow %s %d", render.Safe(args[0]), s.Number)
			a.nextStep(ctx, cl, org)
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "only check that it can start, and where it would stop")
	c.Flags().StringVar(&instruction, "instruction", "", "what to look at first")
	c.Flags().BoolVar(&follow, "follow", false, "then follow it until it ends, as hkr scans follow")
	c.Flags().StringVar(&failOn, "fail-on", "", "with --follow: exit 4 if it finds anything this severe")
	addLimitsFlags(c, &preset, &spend, &duration)
	return c
}

// completionText is how a scan ended, in words; an unknown code as is.
func completionText(code string) string {
	switch code {
	case "released", "agent_done": // agent_done: older scans, the same thing.
		return "finished early: HKr stopped sending it back with budget left"
	case "budget_used":
		return "used its budget"
	case "asks_exhausted":
		return "its answers stopped turning up anything new"
	case "duration_exceeded":
		return "past the time limit you set"
	case "window_closed":
		return "finished at the end of its working hours"
	}
	return render.Safe(code)
}

// earlyStop is when a preset stops before its spending ceiling.
var earlyStop = map[string]string{
	"first_look":       ", or earlier on a confirmed high",
	"confidence_check": ", or earlier at the first medium it confirms",
	"deep_run":         " unless highs come in a burst",
	"until_critical":   ", or earlier on its count of highs",
}

// quoteText is where a scan would stop: "it stops at $40.00, or earlier
// on a confirmed high (balance $120.00; starting needs $10.00)". preset
// is --preset, when given.
func quoteText(q *api.ScanQuote, preset string) string {
	if q.MaxSpendUSD == "" {
		stops := "it stops when your balance ($" + q.BalanceUSD + ") runs out"
		if preset == "until_critical" {
			stops = "it stops on its count of highs, else when your balance ($" + q.BalanceUSD + ") runs out"
		}
		return "it has no spending ceiling: " + stops + " (starting needs $" + q.StartFloorUSD + ")"
	}
	return fmt.Sprintf("it stops at $%s%s (balance $%s; starting needs $%s)", q.MaxSpendUSD, earlyStop[preset], q.BalanceUSD, q.StartFloorUSD)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func newScansSteerCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "steer <domain> [<number>] <message>",
		Short: "Send a running scan's agent a message",
		Long: "Send the agent of a running scan (the latest by default) a message; it\n" +
			"reads it on its next turn.",
		Example: "  hkr scans steer shop.example.com 'Look at the admin panel next.'\n" +
			"  hkr scans steer shop.example.com 3 'Skip the blog.'",
		Args: argsBetween(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, msg := "", args[1]
			if len(args) == 3 {
				number, msg = args[1], args[2]
			}
			if strings.TrimSpace(msg) == "" {
				return usageError{errors.New("steer needs a message for the agent")}
			}
			ctx := cmd.Context()
			r, err := a.scanRef(ctx, args[0], number)
			if err != nil {
				return err
			}
			if err := r.resolveLatest(ctx); err != nil {
				return err
			}
			_, raw, err := r.cl.ScanVerb(ctx, r.org, r.domain, r.number, "steer", map[string]string{"message": msg})
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("Sent to %s.", r.name())
			return nil
		},
	}
}

// newScanVerbCmd is wind-down, stop or resume: the scan and nothing else.
// ask makes it confirm first, or need --yes.
func newScanVerbCmd(a *app, verb, short string, ask bool, done string) *cobra.Command {
	return &cobra.Command{
		Use:     verb + " <domain> [<number>]",
		Short:   short,
		Example: "  hkr scans " + verb + " shop.example.com\n  hkr scans " + verb + " shop.example.com 3",
		Args:    argsBetween(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ask && !a.flags.yes && !a.env.StdinIsTerminal {
				return usageError{fmt.Errorf("pass --yes to %s it without a terminal", verb)}
			}
			ctx := cmd.Context()
			r, err := a.scanRef(ctx, args[0], argAt(args, 1))
			if err != nil {
				return err
			}
			if err := r.resolveLatest(ctx); err != nil {
				return err
			}
			if ask {
				if err := a.confirm(fmt.Sprintf("%s %s?", upperFirst(verb), r.name()), "pass --yes to "+verb+" it without a terminal"); err != nil {
					return err
				}
			}
			_, raw, err := r.cl.ScanVerb(ctx, r.org, r.domain, r.number, verb, nil)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("%s %s.", upperFirst(r.name()), done)
			return nil
		},
	}
}
