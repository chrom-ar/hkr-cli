package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// Scan is one of a domain's scans, as lists and verbs show it; the last
// three fields come with one scan.
type Scan struct {
	ID                 string `json:"id"`
	DomainID           string `json:"domain_id"`
	Number             int    `json:"number"`
	Status             string `json:"status"`
	PresetKey          string `json:"preset_key"`
	MaxSpendUSD        string `json:"max_spend_usd"`
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	CostUSD            string `json:"cost_usd"`
	StartedAt          string `json:"started_at"`
	FinishedAt         string `json:"finished_at"`
	DurationMS         int64  `json:"duration_ms"`
	PausedReason       string `json:"paused_reason"`
	WindingDown        bool   `json:"winding_down"`
	Stopping           bool   `json:"stopping"`
	Findings           struct {
		New  int `json:"new"`
		Seen int `json:"seen"`
	} `json:"findings"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`

	Instruction string `json:"instruction"`
	Completion  string `json:"completion"`
	ErrorText   string `json:"error_text"`

	Raw json.RawMessage `json:"-"` // As HKr sent it.
}

// Over reports whether the scan ended.
func (s Scan) Over() bool {
	return s.Status == "completed" || s.Status == "failed" || s.Status == "cancelled"
}

// ScanQuote is a scan's dry run.
type ScanQuote struct {
	CanStart        bool   `json:"can_start"`
	MaxSpendUSD     string `json:"max_spend_usd"`
	BalanceUSD      string `json:"balance_usd"`
	StartFloorUSD   string `json:"start_floor_usd"`
	PartiallyFunded bool   `json:"partially_funded"`
}

// Step is a milestone in a scan's timeline.
type Step struct {
	ID         string `json:"id"`
	Importance string `json:"importance"`
	Summary    string `json:"summary"`
	Target     string `json:"target"`
	At         string `json:"at"`

	Raw json.RawMessage `json:"-"` // As HKr sent it.
}

// ScansPath is /orgs/:org/domains/:domain/scans.
func ScansPath(org, domain string) string { return DomainPath(org, domain) + "/scans" }

// ScanPath is …/scans/:number; number 0 is "latest".
func ScanPath(org, domain string, number int) string {
	n := "latest"
	if number > 0 {
		n = strconv.Itoa(number)
	}
	return ScansPath(org, domain) + "/" + n
}

func (c *Client) scan(ctx context.Context, method, path string, q Query, body any) (*Scan, []byte, error) {
	var s Scan
	raw, err := c.Send(ctx, method, path, q, body, &s)
	if err != nil {
		return nil, nil, err
	}
	return &s, raw, nil
}

// Scan is GET …/scans/:number (0: the latest).
func (c *Client) Scan(ctx context.Context, org, domain string, number int) (*Scan, []byte, error) {
	return c.scan(ctx, http.MethodGet, ScanPath(org, domain, number), nil, nil)
}

// StartScan is POST …/scans: it spends credit.
func (c *Client) StartScan(ctx context.Context, org, domain string, body map[string]any) (*Scan, []byte, error) {
	var b any
	if len(body) > 0 {
		b = body
	}
	return c.scan(ctx, http.MethodPost, ScansPath(org, domain), nil, b)
}

// QuoteScan is POST …/scans?dry_run=true.
func (c *Client) QuoteScan(ctx context.Context, org, domain string, body map[string]any) (*ScanQuote, []byte, error) {
	var b any
	if len(body) > 0 {
		b = body
	}
	var q ScanQuote
	raw, err := c.Send(ctx, http.MethodPost, ScansPath(org, domain), Query{"dry_run": "true"}, b, &q)
	if err != nil {
		return nil, nil, err
	}
	return &q, raw, nil
}

// ScanVerb POSTs steer, wind-down, stop or resume.
func (c *Client) ScanVerb(ctx context.Context, org, domain string, number int, verb string, body any) (*Scan, []byte, error) {
	return c.scan(ctx, http.MethodPost, ScanPath(org, domain, number)+"/"+verb, nil, body)
}

// Steps reads a scan's timeline from cursor ("" for the start) until an
// empty page: while a scan runs the cursor never ends. It returns the
// steps and the cursor for newer ones ("" once the scan is over).
func (c *Client) Steps(ctx context.Context, org, domain string, number int, cursor string) ([]Step, string, error) {
	var steps []Step
	for {
		var page struct {
			Data       []Step `json:"data"`
			NextCursor string `json:"next_cursor"`
		}
		if _, err := c.Get(ctx, ScanPath(org, domain, number)+"/steps", Query{"cursor": cursor}, &page); err != nil {
			return nil, "", err
		}
		steps = append(steps, page.Data...)
		cursor = page.NextCursor
		if len(page.Data) == 0 || cursor == "" {
			return steps, cursor, nil
		}
	}
}
