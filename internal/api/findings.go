package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// DomainRef is the domain a finding belongs to.
type DomainRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	RootDomain string `json:"root_domain"`
}

// Finding is a finding as lists and changes show it. Detail fields come
// with one finding (GET …/findings/:id).
type Finding struct {
	ID             string     `json:"id"`
	Ref            string     `json:"ref"`
	Title          string     `json:"title"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	State          string     `json:"state"`
	Class          string     `json:"class"`
	Source         string     `json:"source"`
	CVSSScore      *float64   `json:"cvss_score"`
	CVSSVector     string     `json:"cvss_vector"`
	Domain         DomainRef  `json:"domain"`
	Target         string     `json:"target"`
	FixLivesIn     string     `json:"fix_lives_in"`
	SuggestedFixed *Suggested `json:"suggested_fixed"`
	FirstSeenAt    string     `json:"first_seen_at"`
	ConfirmedAt    string     `json:"confirmed_at"`
	UpdatedAt      string     `json:"updated_at"`
	URL            string     `json:"url"`

	Locale                string    `json:"locale"`
	Summary               string    `json:"summary"`
	Affected              string    `json:"affected"`
	Reproduction          string    `json:"reproduction"`
	Impact                string    `json:"impact"`
	Remediation           string    `json:"remediation"`
	SeverityJustification string    `json:"severity_justification"`
	FirstSeenScan         *ScanRef  `json:"first_seen_scan"`
	LastSeenScan          *ScanRef  `json:"last_seen_scan"`
	Accepted              *Accepted `json:"accepted"`

	Raw json.RawMessage `json:"-"` // As HKr sent it.
}

// Suggested is HKr's suggestion that a change fixed the finding, waiting
// for someone to confirm it.
type Suggested struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
	URL    string `json:"url"`
	At     string `json:"at"`
}

// ScanRef names a scan by its domain and number.
type ScanRef struct {
	DomainID string `json:"domain_id"`
	Number   int    `json:"number"`
}

// Accepted is who accepted a finding's risk, when and why.
type Accepted struct {
	Reason string `json:"reason"`
	At     string `json:"at"`
	By     *User  `json:"by"`
}

// Evidence is one evidence file of a finding, from one scan.
type Evidence struct {
	Path        string `json:"path"`
	Scan        int    `json:"scan"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// Fix is an agent-written patch for a finding, and its pull request.
type Fix struct {
	ID               string       `json:"id"`
	FindingID        string       `json:"finding_id"`
	Status           string       `json:"status"`
	UnverifiedReason string       `json:"unverified_reason"`
	Repository       *RepoRef     `json:"repository"`
	PullRequest      *PullRequest `json:"pull_request"`
	CreatedAt        string       `json:"created_at"`
	StartedAt        string       `json:"started_at"`
	FinishedAt       string       `json:"finished_at"`
}

// Done reports whether the fix stopped working: proven, verified, or
// given up one way or another.
func (f Fix) Done() bool { return f.Status != "pending" && f.Status != "running" }

// RepoRef names a repository.
type RepoRef struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
}

// PullRequest is a fix's pull request.
type PullRequest struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	State  string `json:"state"`
}

// FixQuote is a fix's dry run: whether it can start, and what it may
// cost (EstimateUSD is empty when HKr can't say).
type FixQuote struct {
	CanStart    bool   `json:"can_start"`
	EstimateUSD string `json:"estimate_usd"`
	BalanceUSD  string `json:"balance_usd"`
}

// FindingPath is /orgs/:org/findings/:id.
func FindingPath(org, id string) string { return OrgPath(org) + "/findings/" + Seg(id) }

// FixPath is /orgs/:org/fixes/:id.
func FixPath(org, id string) string { return OrgPath(org) + "/fixes/" + Seg(id) }

// Finding is GET …/findings/:id.
func (c *Client) Finding(ctx context.Context, org, id string) (*Finding, []byte, error) {
	var f Finding
	raw, err := c.Get(ctx, FindingPath(org, id), nil, &f)
	if err != nil {
		return nil, nil, err
	}
	return &f, raw, nil
}

// ChangeFinding POSTs one of a finding's changes (mark-fixed, accept,
// reopen) and answers the finding.
func (c *Client) ChangeFinding(ctx context.Context, org, id, action string, body any) (*Finding, []byte, error) {
	var f Finding
	raw, err := c.Send(ctx, http.MethodPost, FindingPath(org, id)+"/"+action, nil, body, &f)
	if err != nil {
		return nil, nil, err
	}
	return &f, raw, nil
}

// QuoteFix is POST …/findings/:id/fixes?dry_run=true.
func (c *Client) QuoteFix(ctx context.Context, org, id string) (*FixQuote, []byte, error) {
	var q FixQuote
	raw, err := c.Send(ctx, http.MethodPost, FindingPath(org, id)+"/fixes", Query{"dry_run": "true"}, nil, &q)
	if err != nil {
		return nil, nil, err
	}
	return &q, raw, nil
}

// ProposeFix is POST …/findings/:id/fixes: it spends credit.
func (c *Client) ProposeFix(ctx context.Context, org, id string) (*Fix, []byte, error) {
	var f Fix
	raw, err := c.Send(ctx, http.MethodPost, FindingPath(org, id)+"/fixes", nil, nil, &f)
	if err != nil {
		return nil, nil, err
	}
	return &f, raw, nil
}

// Fix is GET /orgs/:org/fixes/:id.
func (c *Client) Fix(ctx context.Context, org, id string) (*Fix, []byte, error) {
	var f Fix
	raw, err := c.Get(ctx, FixPath(org, id), nil, &f)
	if err != nil {
		return nil, nil, err
	}
	return &f, raw, nil
}

// OpenPullRequest is POST /orgs/:org/fixes/:id/pull-request.
func (c *Client) OpenPullRequest(ctx context.Context, org, id string) (*Fix, []byte, error) {
	var f Fix
	raw, err := c.Send(ctx, http.MethodPost, FixPath(org, id)+"/pull-request", nil, nil, &f)
	if err != nil {
		return nil, nil, err
	}
	return &f, raw, nil
}
