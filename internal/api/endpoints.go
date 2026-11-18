package api

import (
	"context"
	"net/http"
	"net/url"
)

// Seg escapes one path segment: a slug, an id.
func Seg(s string) string { return url.PathEscape(s) }

// OrgPath is /orgs/:org.
func OrgPath(org string) string { return "/orgs/" + Seg(org) }

// Me is GET /me.
func (c *Client) Me(ctx context.Context) (*Me, []byte, error) {
	var me Me
	raw, err := c.Get(ctx, "/me", nil, &me)
	if err != nil {
		return nil, nil, err
	}
	return &me, raw, nil
}

// RevokeToken is DELETE /me/token: the token making the call.
func (c *Client) RevokeToken(ctx context.Context) error {
	_, err := c.Send(ctx, http.MethodDelete, "/me/token", nil, nil, nil)
	return err
}

// Orgs is GET /orgs: the person's organizations.
func (c *Client) Orgs(ctx context.Context, q Query, all bool) (*Page[Organization], error) {
	return List[Organization](ctx, c, "/orgs", q, all)
}

// Org is GET /orgs/:org.
func (c *Client) Org(ctx context.Context, org string) (*Org, []byte, error) {
	var o Org
	raw, err := c.Get(ctx, OrgPath(org), nil, &o)
	if err != nil {
		return nil, nil, err
	}
	return &o, raw, nil
}
