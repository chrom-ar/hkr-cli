package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Download fetches a file under /api/v1: HKr answers it, or redirects to
// short-lived storage elsewhere, which is fetched without the token.
func (c *Client) Download(ctx context.Context, path string, q Query) ([]byte, error) {
	u, err := c.url(path, q)
	if err != nil {
		return nil, err
	}
	resp, data, err := c.attempt(ctx, http.MethodGet, u, nil, "*/*")
	if err != nil {
		return nil, err
	}
	switch s := resp.StatusCode; {
	case s >= 200 && s < 300:
		return data, nil
	case s == http.StatusFound || s == http.StatusSeeOther || s == http.StatusTemporaryRedirect:
		return c.fetchStorage(ctx, u, resp.Header.Get("Location"), s)
	}
	return nil, c.parseError(resp, data)
}

// fetchStorage GETs a storage URL HKr redirected to, with no token and no
// further redirects.
func (c *Client) fetchStorage(ctx context.Context, from *url.URL, location string, status int) ([]byte, error) {
	next, err := from.Parse(location)
	if location == "" || err != nil || (next.Scheme != "https" && next.Scheme != "http") || next.Host == "" {
		return nil, &Error{Status: status, Code: "http_error", Message: "HKr redirected the download to an invalid Location"}
	}
	actx, cancel := context.WithTimeout(ctx, c.o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, next.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building the download request: %w", err)
	}
	if c.o.UserAgent != "" {
		req.Header.Set("User-Agent", c.o.UserAgent)
	}
	host := strings.ToLower(next.Host)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Code: "network", Message: fmt.Sprintf("couldn't download from %s (%v)", host, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	c.o.Logger.Debug("download", "host", host, "status", resp.StatusCode)
	if resp.StatusCode >= 300 {
		return nil, &Error{Status: resp.StatusCode, Code: "http_error",
			Message: fmt.Sprintf("%s refused the download (%d); the link may have expired, try again", host, resp.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.o.MaxBody+1))
	if err != nil {
		return nil, &Error{Code: "network", Message: fmt.Sprintf("couldn't download from %s (%v)", host, err)}
	}
	if int64(len(data)) > c.o.MaxBody {
		return nil, &Error{Status: resp.StatusCode, Code: "http_error", Message: "the file is too large"}
	}
	return data, nil
}
