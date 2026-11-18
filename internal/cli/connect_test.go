package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
)

// A server whose connections never change.
func noConnections(t *testing.T) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": [], "next_cursor": null}`))
	}))
	t.Cleanup(srv.Close)
	cl, err := api.New(api.Options{URL: srv.URL, Token: "hkrp_test"})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

// The person closed the tab: waiting ends after 15 minutes, saying how
// to try again.
func TestWaitConnectionGivesUp(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	a := &app{env: withDefaults(Env{
		Now: func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error {
			now = now.Add(d)
			return nil
		},
	})}
	_, err := a.waitConnection(t.Context(), noConnections(t), "acme", map[string]bool{}, "http://hkr.test/vcs/github/connect?org=acme")
	if err == nil || !strings.Contains(err.Error(), "no connection appeared in 15m; open the URL again: http://hkr.test/vcs/github/connect?org=acme") {
		t.Fatalf("err = %v", err)
	}
	if exitCode(err) != ExitError {
		t.Errorf("exit = %d", exitCode(err))
	}
}

// Ctrl-C stops waiting.
func TestWaitConnectionCtrlC(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	a := &app{env: withDefaults(Env{Sleep: func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}})}
	_, err := a.waitConnection(ctx, noConnections(t), "acme", map[string]bool{}, "u")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
