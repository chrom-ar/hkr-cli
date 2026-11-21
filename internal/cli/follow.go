package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/channel"
	"github.com/chrom-ar/hkr-cli/internal/follow"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newScansFollowCmd(a *app) *cobra.Command {
	var failOn, detail string
	c := &cobra.Command{
		Use:   "follow <domain> [<number>]",
		Short: "Follow a scan live until it ends (the latest by default)",
		Long: "Print a scan's timeline, then its steps, status changes and findings as\n" +
			"they happen, until it ends. Exits 0 when it completed, 4 when it failed\n" +
			"or was cancelled, or when --fail-on high (or another severity) matches\n" +
			"an open finding it saw: for CI. Ctrl-C stops following, not the scan.\n" +
			"--json prints one event per line.",
		Example: "  hkr scans follow shop.example.com\n" +
			"  hkr scans follow shop.example.com 3 --fail-on high\n" +
			"  hkr scans start shop.example.com --yes --follow --fail-on high",
		Args: argsBetween(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := severityQuery(failOn); err != nil {
				return err
			}
			if err := oneOf("--detail", detail, "key", "normal", "all"); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := a.scanRef(ctx, args[0], argAt(args, 1))
			if err != nil {
				return err
			}
			if err := r.resolveLatest(ctx); err != nil {
				return err
			}
			return a.followScan(ctx, r, failOn, detail)
		},
	}
	c.Flags().StringVar(&failOn, "fail-on", "", "exit 4 if the scan saw an open finding this severe (high: high or critical)")
	c.Flags().StringVar(&detail, "detail", "", "key, normal (the default: no noise) or all")
	return c
}

// scanREST is follow.REST for one scan.
type scanREST struct{ r scanRef }

func (s scanREST) Scan(ctx context.Context) (*api.Scan, error) {
	scan, _, err := s.r.cl.Scan(ctx, s.r.org, s.r.domain, s.r.number)
	return scan, err
}

func (s scanREST) Steps(ctx context.Context, cursor string) ([]api.Step, string, error) {
	return s.r.cl.Steps(ctx, s.r.org, s.r.domain, s.r.number, cursor)
}

// event is one line of follow --json: {"event": name, "data": HKr's
// JSON as is}.
func (a *app) event(name string, data json.RawMessage) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return err
	}
	_, err := fmt.Fprintf(a.env.Stdout, "{\"event\":%q,\"data\":%s}\n", name, compact.Bytes())
	return err
}

func (a *app) followScan(ctx context.Context, r scanRef, failOn, detail string) error {
	s, err := a.resolve()
	if err != nil {
		return err
	}
	lastStatus := ""
	o := follow.Options{
		REST: scanREST{r},
		Dial: func(ctx context.Context, onPush func(channel.Push)) (*channel.Conn, error) {
			return channel.Dial(ctx, channel.Options{
				URL: s.url, Token: s.token.Reveal(), UserAgent: a.userAgent(),
				HTTPClient: a.env.HTTPClient, OnPush: onPush,
			})
		},
		URL:   s.url,
		Topic: func(scan *api.Scan) string { return "scan:" + r.org + "/" + scan.ID },
		Step: func(st api.Step) error {
			if a.flags.json {
				return a.event("step", st.Raw)
			}
			if detail != "all" && (st.Importance == "noise" || (detail == "key" && st.Importance != "key")) {
				return nil
			}
			a.out("%s", a.stepLine(st))
			return nil
		},
		Scan: func(scan api.Scan) {
			if a.flags.json {
				_ = a.event("scan", scan.Raw)
				return
			}
			if st := a.scanStatus(&scan); st != lastStatus {
				lastStatus = st
				a.progress("Scan %d: %s, $%s so far.", r.number, st, scan.CostUSD)
			}
		},
		Finding: func(f api.Finding) {
			if a.flags.json {
				_ = a.event("finding", f.Raw)
				return
			}
			a.out("%s  Found %s: %s (%s)", render.Time(f.UpdatedAt, a.env.Location, "15:04"), f.Severity, render.Safe(f.Title), f.ID)
		},
		Reconnecting: func(in time.Duration, cause error) {
			a.progress("The live stream dropped (%s); trying again in %s…", render.Safe(cause.Error()), in)
		},
		Sleep: a.env.Sleep,
	}
	end, err := follow.Run(ctx, o)
	if errors.Is(err, context.Canceled) {
		return nil // Ctrl-C stops following; the scan goes on.
	}
	if err != nil {
		return err
	}
	if a.flags.json {
		if err := a.event("ended", end.Raw); err != nil {
			return err
		}
	} else {
		how := end.Status
		if end.Completion != "" {
			how += " (" + completionText(end.Completion) + ")"
		}
		noun := "findings"
		if end.Findings.New == 1 {
			noun = "finding"
		}
		a.out("Scan %d of %s %s: %d new %s, $%s.", end.Number, render.Safe(r.arg), how, end.Findings.New, noun, end.CostUSD)
		if end.ErrorText != "" {
			a.progress("%s", render.Safe(end.ErrorText))
		}
	}
	if end.Status != "completed" {
		return silentError{ExitScan}
	}
	return a.failOn(ctx, r, end.Number, failOn)
}

// failOn exits 4 when the scan saw an open finding at or above the
// severity.
func (a *app) failOn(ctx context.Context, r scanRef, number int, severity string) error {
	sev, err := severityQuery(severity)
	if err != nil || sev == "" {
		return err
	}
	page, err := api.List[api.Finding](ctx, r.cl, api.OrgPath(r.org)+"/findings",
		api.Query{"domain": r.domain, "scan": fmt.Sprint(number), "severity": sev}, true)
	if err != nil {
		return err
	}
	if len(page.Items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(page.Items))
	for _, f := range page.Items {
		ids = append(ids, f.ID)
	}
	noun := "findings"
	if len(ids) == 1 {
		noun = "finding"
	}
	fmt.Fprintf(a.env.Stderr, "%d %s at or above %s: %s\n", len(ids), noun, severity, strings.Join(ids, ", "))
	return silentError{ExitScan}
}
