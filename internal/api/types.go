package api

import "encoding/json"

// The shapes below follow HKr's API, which HKr adopts for signing
// in and /me (SPEC 7.1). ../hkr/docs/api.md is the contract once
// written: where it differs, it wins.

// User is a person, as /me and the device flow show them. Name is the
// first name; LastName may be empty.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LastName string `json:"last_name"`
	Email    string `json:"email"`
	Locale   string `json:"locale,omitempty"`
}

// FullName is the first and last name, as given.
func (u User) FullName() string { return fullName(u.Name, u.LastName) }

func fullName(first, last string) string {
	if first == "" || last == "" {
		return first + last
	}
	return first + " " + last
}

func unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// Me is GET /me: who the token is.
type Me struct {
	User          User           `json:"user"`
	Token         TokenInfo      `json:"token"`
	Organizations []Organization `json:"organizations"`
}

// TokenInfo is the token's name, access ("write" or "read") and the
// organization it's narrowed to ("" when not narrowed).
type TokenInfo struct {
	Name         string `json:"name"`
	Access       string `json:"access"`
	Organization string `json:"organization"`
	Setup        bool   `json:"setup"` // May change schedules, repository settings and links.
}

// Organization is one of the person's organizations, with their role
// ("admin" or "member") and its language. /orgs adds its time zone,
// creation time and open critical and high findings.
type Organization struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	Locale       string `json:"locale"`
	TimeZone     string `json:"time_zone,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	OpenFindings int    `json:"open_findings"`
}

// Org is GET /orgs/:org: the organization and its credit (USD, two
// decimals). Plan is the plan's key, or nil when it has none and can't
// spend.
type Org struct {
	Organization
	BalanceUSD string  `json:"balance_usd"`
	Plan       *string `json:"plan"`
}
