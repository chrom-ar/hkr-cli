package api

import (
	"context"
	"net/http"
)

// Domain is a domain HKr tests (an Engagement in HKr's code), as lists
// and changes show it; the scope fields come with one domain.
type Domain struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	RootDomain         string         `json:"root_domain"`
	Status             string         `json:"status"`
	ScopeBreadth       string         `json:"scope_breadth"`
	Archived           bool           `json:"archived"`
	Verified           bool           `json:"verified"`
	Attested           bool           `json:"attested"`
	PresetKey          string         `json:"preset_key"`
	MaxSpendUSD        string         `json:"max_spend_usd"`
	MaxDurationSeconds int            `json:"max_duration_seconds"`
	OpenFindings       map[string]int `json:"open_findings"`
	LastScan           *ScanSummary   `json:"last_scan"`
	CreatedAt          string         `json:"created_at"`
	URL                string         `json:"url"`

	OutOfScope     []string `json:"out_of_scope"`
	Constraints    string   `json:"constraints"`
	WindowStartsAt string   `json:"window_starts_at"`
	WindowEndsAt   string   `json:"window_ends_at"`
	ReportLocale   string   `json:"report_locale"`
}

// ScanSummary is a domain's last scan.
type ScanSummary struct {
	Number     int    `json:"number"`
	Status     string `json:"status"`
	FinishedAt string `json:"finished_at"`
}

// Target is something HKr mapped of a domain.
type Target struct {
	ID           string   `json:"id"`
	Value        string   `json:"value"`
	Kind         string   `json:"kind"`
	InScope      bool     `json:"in_scope"`
	DiscoveredBy string   `json:"discovered_by"`
	DiscoveredAt string   `json:"discovered_at"`
	ResolvedIPs  []string `json:"resolved_ips"`
}

// Verification is whether the organization proved it controls a domain,
// and the challenge open (or last tried) for it.
type Verification struct {
	Verified  bool       `json:"verified"`
	Challenge *Challenge `json:"challenge"`
}

// Challenge is a domain verification challenge. Record comes with
// dns_txt, File with http_file, SentTo with email.
type Challenge struct {
	Method string `json:"method"`
	Status string `json:"status"`
	Domain string `json:"domain"`
	Record *struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"record"`
	File *struct {
		URL  string `json:"url"`
		Body string `json:"body"`
	} `json:"file"`
	SentTo        string `json:"sent_to"`
	LastCheckedAt string `json:"last_checked_at"`
	LastError     string `json:"last_error"`
	ExpiresAt     string `json:"expires_at"`
}

// Attestation is the statement someone signs to say they're authorized
// to test a domain, and who signed it.
type Attestation struct {
	Attested       bool   `json:"attested"`
	Statement      string `json:"statement"`
	AttestedByName string `json:"attested_by_name"`
	AttestedAt     string `json:"attested_at"`
}

// DomainPath is /orgs/:org/domains/:id.
func DomainPath(org, id string) string { return OrgPath(org) + "/domains/" + Seg(id) }

// Domain is GET …/domains/:id.
func (c *Client) Domain(ctx context.Context, org, id string) (*Domain, []byte, error) {
	var d Domain
	raw, err := c.Get(ctx, DomainPath(org, id), nil, &d)
	if err != nil {
		return nil, nil, err
	}
	return &d, raw, nil
}

// SendDomain sends a change that answers a domain (create, update).
func (c *Client) SendDomain(ctx context.Context, method, path string, body any) (*Domain, []byte, error) {
	var d Domain
	raw, err := c.Send(ctx, method, path, nil, body, &d)
	if err != nil {
		return nil, nil, err
	}
	return &d, raw, nil
}

// Verification sends a request about a domain's verification: GET it,
// POST to start one, POST …/check.
func (c *Client) Verification(ctx context.Context, method, path string, body any) (*Verification, []byte, error) {
	var v Verification
	raw, err := c.Send(ctx, method, path, nil, body, &v)
	if err != nil {
		return nil, nil, err
	}
	return &v, raw, nil
}

// Attestation is GET …/attestation, or with a body, POST.
func (c *Client) Attestation(ctx context.Context, org, id string, body any) (*Attestation, []byte, error) {
	var a Attestation
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	raw, err := c.Send(ctx, method, DomainPath(org, id)+"/attestation", nil, body, &a)
	if err != nil {
		return nil, nil, err
	}
	return &a, raw, nil
}
