package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

// parseAt reads --at: HH:MM, 24-hour.
func parseAt(s string) (string, error) {
	bad := usageError{fmt.Errorf("invalid --at %q: like 03:00 (24-hour, the organization's time zone)", s)}
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(m) != 2 || h == "" || len(h) > 2 {
		return "", bad
	}
	hour, err1 := strconv.Atoi(h)
	minute, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return "", bad
	}
	return fmt.Sprintf("%02d:%02d", hour, minute), nil
}

// parseWeekday reads --weekday: mon, Monday, MON … as HKr's "mon".
func parseWeekday(s string) (string, error) {
	day := strings.ToLower(s)
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		if len(day) >= 3 && strings.HasPrefix(fullDay(d), day) {
			return d, nil
		}
	}
	return "", usageError{fmt.Errorf("unknown --weekday %q: use mon, tue, wed, thu, fri, sat or sun", s)}
}

func fullDay(d string) string {
	return map[string]string{"mon": "monday", "tue": "tuesday", "wed": "wednesday", "thu": "thursday",
		"fri": "friday", "sat": "saturday", "sun": "sunday"}[d]
}

// everyText is "week (mon)", "2 days", "month (day 1)", "once".
func everyText(s api.Schedule) string {
	text := s.Period
	if s.Interval > 1 {
		text = fmt.Sprintf("%d %ss", s.Interval, s.Period)
	}
	switch {
	case s.Period == "week" && s.Weekday != "":
		text += " (" + s.Weekday + ")"
	case s.Period == "month" && s.DayOfMonth > 0:
		text += fmt.Sprintf(" (day %d)", s.DayOfMonth)
	}
	return text
}

func scheduleLimits(s api.Schedule) string {
	var parts []string
	if s.PresetKey != "" {
		parts = append(parts, s.PresetKey)
	}
	if s.MaxSpendUSD != "" {
		parts = append(parts, "up to $"+s.MaxSpendUSD)
	}
	if s.MaxDurationSeconds > 0 {
		parts = append(parts, "up to "+durationText(secondsDuration(s.MaxDurationSeconds)))
	}
	if len(parts) == 0 {
		return "the domain's"
	}
	return strings.Join(parts, ", ")
}

func newSchedulesCmd(a *app) *cobra.Command {
	return group(a, "schedules", "A domain's recurring scans",
		"  hkr schedules list shop.example.com\n"+
			"  hkr schedules create shop.example.com --every week --weekday mon --at 03:00",
		newSchedulesListCmd(a), newSchedulesCreateCmd(a),
		newScheduleVerbCmd(a, "pause", "Stop a schedule firing, keeping it", false),
		newScheduleVerbCmd(a, "resume", "Start a paused schedule again (it spends unattended)", true),
		newScheduleVerbCmd(a, "delete", "Remove a schedule", true))
}

func newSchedulesListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list <domain>",
		Short:   "A domain's schedules",
		Example: "  hkr schedules list shop.example.com",
		Args:    argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, org, id, err := a.inDomain(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printList(cmd.Context(), a, cl, &l, api.DomainPath(org, id)+"/schedules", nil, func(ss []api.Schedule) string {
				rows := make([][]string, 0, len(ss))
				for _, s := range ss {
					status := "active"
					if s.Paused {
						status = "paused"
					}
					next := render.Time(s.NextAt, a.env.Location, "2 Jan 2006 15:04")
					if next == "" {
						next = "—"
					}
					rows = append(rows, []string{s.ID, everyText(s), s.Time, next, scheduleLimits(s), status})
				}
				if len(ss) > 0 {
					a.progress("Times are in %s.", render.Safe(ss[0].TimeZone))
				}
				return render.Table([]string{"ID", "EVERY", "AT", "NEXT", "LIMITS", "STATUS"}, rows)
			}, "No schedules. Add one: hkr schedules create "+render.Safe(args[0])+" --every week --weekday mon --at 03:00")
		},
	}
	l.add(c)
	return c
}

func newSchedulesCreateCmd(a *app) *cobra.Command {
	var (
		every, weekday, at, once string
		interval, day            int
		preset, spend, duration  string
	)
	c := &cobra.Command{
		Use:   "create <domain>",
		Short: "Scan a domain on a schedule (it spends unattended)",
		Long: "Scan a domain every day, week or month (every --interval of them) at\n" +
			"--at, in the organization's time zone; or once, at --once (a time with\n" +
			"its zone). Each scan has its own limits, else the domain's. A schedule\n" +
			"spends credit unattended: it asks first, or takes --yes. Needs a token\n" +
			"with setup changes.",
		Example: "  hkr schedules create shop.example.com --every week --weekday mon --at 03:00\n" +
			"  hkr schedules create shop.example.com --every month --day 1 --at 02:30 --max-spend 100\n" +
			"  hkr schedules create shop.example.com --once 2026-12-01T06:00:00Z",
		Args: argsBetween(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := limitsBody(preset, spend, duration)
			if err != nil {
				return err
			}
			switch {
			case once != "" && every != "":
				return usageError{errors.New("--once and --every don't go together")}
			case once != "":
				t, err := time.Parse(time.RFC3339, once)
				if err != nil {
					return usageError{fmt.Errorf("invalid --once %q: a time with its zone, like 2026-12-01T06:00:00Z", once)}
				}
				body["period"], body["starts_at"] = "once", t.UTC().Format(time.RFC3339)
			case every == "":
				return usageError{errors.New("say when: --every day|week|month with --at, or --once")}
			default:
				if err := oneOf("--every", every, "day", "week", "month"); err != nil {
					return err
				}
				hhmm, err := parseAt(at)
				if err != nil {
					return err
				}
				if interval < 1 {
					return usageError{errors.New("--interval must be 1 or more")}
				}
				body["period"], body["time"], body["interval"] = every, hhmm, interval
				switch every {
				case "week":
					if weekday == "" {
						return usageError{errors.New("--every week needs --weekday (mon … sun)")}
					}
					if body["weekday"], err = parseWeekday(weekday); err != nil {
						return err
					}
				case "month":
					if day < 1 || day > 31 {
						return usageError{errors.New("--every month needs --day, 1 to 31")}
					}
					body["day_of_month"] = day
				}
			}
			if !a.flags.yes && !a.env.StdinIsTerminal {
				return usageError{errors.New("a schedule spends credit unattended: pass --yes to create it without a terminal")}
			}
			ctx := cmd.Context()
			cl, org, id, err := a.inDomain(ctx, args[0])
			if err != nil {
				return err
			}
			if !a.flags.yes {
				tz := "the organization's time zone"
				if o, _, err := cl.Org(ctx, org); err == nil && o.TimeZone != "" {
					tz = render.Safe(o.TimeZone)
				}
				if err := a.confirm(fmt.Sprintf("Scan %s %s? HKr spends this unattended.", render.Safe(args[0]), whenText(body, tz)),
					"a schedule spends credit unattended: pass --yes"); err != nil {
					return err
				}
			}
			var s api.Schedule
			raw, err := cl.Send(ctx, http.MethodPost, api.DomainPath(org, id)+"/schedules", nil, body, &s)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(raw)
			}
			a.out("Scheduled %s: %s.", s.ID, a.scheduleText(s))
			a.nextStep(ctx, cl, org)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&every, "every", "", "day, week or month")
	f.IntVar(&interval, "interval", 1, "every this many days, weeks or months")
	f.StringVar(&weekday, "weekday", "", "with --every week: mon … sun")
	f.IntVar(&day, "day", 0, "with --every month: the day of the month, 1 to 31")
	f.StringVar(&at, "at", "", "the time, HH:MM in the organization's time zone")
	f.StringVar(&once, "once", "", "instead of --every: one scan at this time (RFC 3339, with its zone)")
	addLimitsFlags(c, &preset, &spend, &duration)
	return c
}

// whenText says, before creating, how often it runs and what each scan
// may spend, in tz (the organization's time zone).
func whenText(body map[string]any, tz string) string {
	spend := "the domain's limit"
	if v, ok := body["max_spend_usd"].(string); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			spend = fmt.Sprintf("$%.2f", f)
		}
	}
	if body["period"] == "once" {
		return fmt.Sprintf("once, at %v, each scan up to %s", body["starts_at"], spend)
	}
	s := api.Schedule{Period: fmt.Sprint(body["period"])}
	s.Interval, _ = body["interval"].(int)
	s.DayOfMonth, _ = body["day_of_month"].(int)
	every := strings.TrimSuffix(everyText(s), fmt.Sprintf(" (day %d)", s.DayOfMonth))
	when := "every " + every
	if wd, ok := body["weekday"].(string); ok {
		when += " on " + wd
	}
	if s.DayOfMonth > 0 {
		when += fmt.Sprintf(" on day %d", s.DayOfMonth)
	}
	return fmt.Sprintf("%s at %v (%s), each scan up to %s", when, body["time"], tz, spend)
}

// scheduleText is "every week on mon at 03:00 (zone), next …".
func (a *app) scheduleText(s api.Schedule) string {
	text := "once"
	if s.Period != "once" {
		text = "every " + strings.TrimSuffix(everyText(s), " ("+s.Weekday+")")
		if s.Weekday != "" {
			text += " on " + s.Weekday
		}
		text += " at " + s.Time + " (" + render.Safe(s.TimeZone) + ")"
	}
	if s.NextAt != "" {
		text += ", next " + render.Time(s.NextAt, a.env.Location, "2 Jan 2006 15:04")
	}
	return text
}

// newScheduleVerbCmd is pause, resume or delete; ask makes it confirm.
func newScheduleVerbCmd(a *app, verb, short string, ask bool) *cobra.Command {
	return &cobra.Command{
		Use:     verb + " <domain> <schedule>",
		Short:   short,
		Example: "  hkr schedules " + verb + " shop.example.com Sc2Kq2XpAb2m",
		Args:    argsBetween(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ask && !a.flags.yes && !a.env.StdinIsTerminal {
				return usageError{fmt.Errorf("pass --yes to %s it without a terminal", verb)}
			}
			ctx := cmd.Context()
			cl, org, id, err := a.inDomain(ctx, args[0])
			if err != nil {
				return err
			}
			if ask {
				if err := a.confirm(fmt.Sprintf("%s schedule %s?", upperFirst(verb), args[1]), "pass --yes to "+verb+" it"); err != nil {
					return err
				}
			}
			path := api.DomainPath(org, id) + "/schedules/" + api.Seg(args[1])
			method := http.MethodPost
			if verb == "delete" {
				method = http.MethodDelete
			} else {
				path += "/" + verb
			}
			var s api.Schedule
			raw, err := cl.Send(ctx, method, path, nil, nil, &s)
			if err != nil {
				return err
			}
			if a.flags.json {
				if len(raw) == 0 {
					return nil // A delete answers nothing.
				}
				return a.printJSON(raw)
			}
			switch verb {
			case "resume":
				a.out("Resumed %s: next %s.", args[1], render.Time(s.NextAt, a.env.Location, "2 Jan 2006 15:04"))
			default:
				a.out("%s %s.", map[string]string{"pause": "Paused", "delete": "Deleted"}[verb], args[1])
			}
			return nil
		},
	}
}
