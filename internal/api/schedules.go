package api

// Schedule is a domain's recurring (or one-off) scan. Time is HH:MM in
// TimeZone, the organization's; NextAt is UTC, empty when paused or done.
type Schedule struct {
	ID                 string `json:"id"`
	DomainID           string `json:"domain_id"`
	Period             string `json:"period"`
	Interval           int    `json:"interval"`
	Weekday            string `json:"weekday"`
	DayOfMonth         int    `json:"day_of_month"`
	Time               string `json:"time"`
	TimeZone           string `json:"time_zone"`
	PresetKey          string `json:"preset_key"`
	MaxSpendUSD        string `json:"max_spend_usd"`
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	Paused             bool   `json:"paused"`
	NextAt             string `json:"next_at"`
	CreatedAt          string `json:"created_at"`
	// WorkingHours are the weekly ranges it launches in, in TimeZone;
	// each range's end is the scan's deadline. With them, Weekday and
	// Time are empty.
	WorkingHours []WorkingHours `json:"working_hours"`
}

// WorkingHours is a weekly range, HH:MM to HH:MM; an end at or before the
// start is on the next day.
type WorkingHours struct {
	Weekday  string `json:"weekday"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}
