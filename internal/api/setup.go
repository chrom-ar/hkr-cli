package api

import (
	"context"
	"encoding/json"
)

// Setup is an organization's setup checklist, as HKr orders it: what's
// done, what's next, and whether every required step is.
type Setup struct {
	Steps    []SetupStep `json:"steps"`
	Next     *SetupStep  `json:"next"`
	Complete bool        `json:"complete"`
}

// SetupStep is one step of the checklist. Domain is nil for the
// organization-wide ones; Detail is the step's own data, by Key.
type SetupStep struct {
	Key       string          `json:"key"`
	Done      bool            `json:"done"`
	Optional  bool            `json:"optional"`
	CanAct    bool            `json:"can_act"`
	BlockedBy []string        `json:"blocked_by"`
	Domain    *DomainRef      `json:"domain"`
	Detail    json.RawMessage `json:"detail"`
	Minutes   float64         `json:"minutes"`
	URL       string          `json:"url"`
}

// Setup is GET /orgs/:org/setup; domainID "" for every domain.
func (c *Client) Setup(ctx context.Context, org, domainID string) (*Setup, []byte, error) {
	var s Setup
	raw, err := c.Get(ctx, OrgPath(org)+"/setup", Query{"domain": domainID}, &s)
	if err != nil {
		return nil, nil, err
	}
	return &s, raw, nil
}
