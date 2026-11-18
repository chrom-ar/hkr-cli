package cli

import "testing"

// The spending question says exactly how often, and money with cents.
func TestWhenText(t *testing.T) {
	for _, tt := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"period": "week", "interval": 2, "weekday": "mon", "time": "03:00", "max_spend_usd": "100"},
			"every 2 weeks on mon at 03:00 (UTC), each scan up to $100.00"},
		{map[string]any{"period": "month", "interval": 1, "day_of_month": 1, "time": "02:30"},
			"every month on day 1 at 02:30 (UTC), each scan up to the domain's limit"},
		{map[string]any{"period": "once", "starts_at": "2026-12-01T06:00:00Z", "max_spend_usd": "12.5"},
			"once, at 2026-12-01T06:00:00Z, each scan up to $12.50"},
	} {
		if got := whenText(tt.body, "UTC"); got != tt.want {
			t.Errorf("whenText(%v) = %q, want %q", tt.body, got, tt.want)
		}
	}
}
