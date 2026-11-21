package cli

import (
	"strings"
	"testing"

	"github.com/chrom-ar/hkr-cli/internal/api"
)

func TestParseAt(t *testing.T) {
	for in, want := range map[string]string{"03:00": "03:00", "23:59": "23:59", "0:05": "00:05", "9:30": "09:30"} {
		if got, err := parseAt(in); err != nil || got != want {
			t.Errorf("parseAt(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"3pm", "24:00", "12:60", "", "03", "-1:00", "03:00:00"} {
		_, err := parseAt(bad)
		if err == nil || !strings.Contains(err.Error(), `like 03:00 (24-hour, the organization's time zone)`) || exitCode(err) != ExitUsage {
			t.Errorf("parseAt(%q) = %v, want a usage error", bad, err)
		}
	}
}

func TestParseWeekday(t *testing.T) {
	for in, want := range map[string]string{"mon": "mon", "Monday": "mon", "MON": "mon", "sun": "sun", "Thursday": "thu"} {
		if got, err := parseWeekday(in); err != nil || got != want {
			t.Errorf("parseWeekday(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseWeekday("funday"); err == nil || exitCode(err) != ExitUsage {
		t.Errorf("parseWeekday(funday) = %v", err)
	}
}

func TestWorkingHoursText(t *testing.T) {
	hs := []api.WorkingHours{{Weekday: "tue", StartsAt: "20:00", EndsAt: "23:00"}, {Weekday: "sun", StartsAt: "22:00", EndsAt: "06:00"}}
	if got, want := workingHoursText(hs), "Tue 20:00–23:00 · Sun 22:00–Mon 06:00"; got != want {
		t.Errorf("workingHoursText = %q, want %q", got, want)
	}
}
