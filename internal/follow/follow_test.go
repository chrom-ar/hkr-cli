package follow_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/channel"
	"github.com/chrom-ar/hkr-cli/internal/channel/channeltest"
	"github.com/chrom-ar/hkr-cli/internal/follow"
)

const (
	token = "hkrp_follow_test" //nolint:gosec // A fake token.
	topic = "scan:acme/RunScn7Kq2Xp"
	wait  = 5 * time.Second
)

func step(id string) api.Step { return api.Step{ID: id, Importance: "key", Summary: "s-" + id} }

func stepPush(id string) map[string]any {
	return map[string]any{"id": id, "importance": "key", "summary": "s-" + id}
}

// rest is a fake REST API: the scan, and steps answered by cursor.
type rest struct {
	mu       sync.Mutex
	scan     api.Scan
	scanErr  error
	steps    map[string][]api.Step // by cursor; "" is the start
	next     map[string]string     // the cursor after each
	stepsErr []error               // answered first, in order
	cursors  []string              // every cursor asked for
}

func (r *rest) Scan(context.Context) (*api.Scan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scanErr != nil {
		return nil, r.scanErr
	}
	s := r.scan
	return &s, nil
}

func (r *rest) Steps(ctx context.Context, cursor string) ([]api.Step, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cursors = append(r.cursors, cursor)
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(r.stepsErr) > 0 {
		err := r.stepsErr[0]
		r.stepsErr = r.stepsErr[1:]
		return nil, "", err
	}
	next, ok := r.next[cursor]
	if !ok {
		next = cursor
	}
	return r.steps[cursor], next, nil
}

func (r *rest) Cursors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.cursors)
}

func running() api.Scan { return api.Scan{ID: "RunScn7Kq2Xp", Number: 1, Status: "running"} }

// run is one Run in the background, with what it reported.
type run struct {
	t      *testing.T
	cancel context.CancelFunc
	done   chan struct{}
	scan   *api.Scan
	err    error

	mu       sync.Mutex
	steps    []string
	scans    []string
	findings []string
	retries  []time.Duration
}

func (r *run) Steps() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.steps)
}

func (r *run) result() (*api.Scan, error) {
	r.t.Helper()
	select {
	case <-r.done:
		return r.scan, r.err
	case <-time.After(wait):
		r.t.Fatal("Run didn't return")
		return nil, nil
	}
}

func noLeaks(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(wait)
		for runtime.NumGoroutine() > before {
			if time.Now().After(deadline) {
				buf := make([]byte, 1<<20)
				t.Errorf("%d goroutines left behind:\n%s", runtime.NumGoroutine()-before, buf[:runtime.Stack(buf, true)])
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

func dialer(t *testing.T, url string) func(context.Context, func(channel.Push)) (*channel.Conn, error) {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return func(ctx context.Context, onPush func(channel.Push)) (*channel.Conn, error) {
		return channel.Dial(ctx, channel.Options{
			URL: url, Token: token, HTTPClient: &http.Client{Transport: tr},
			ConnectTimeout: wait, JoinTimeout: wait, OnPush: onPush,
		})
	}
}

func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func start(t *testing.T, url string, r *rest, change func(*follow.Options)) *run {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	out := &run{t: t, cancel: cancel, done: make(chan struct{})}
	o := follow.Options{
		REST:  r,
		Dial:  dialer(t, url),
		URL:   url,
		Topic: func(s *api.Scan) string { return "scan:acme/" + s.ID },
		Step: func(s api.Step) error {
			out.mu.Lock()
			defer out.mu.Unlock()
			out.steps = append(out.steps, s.ID)
			return nil
		},
		Scan: func(s api.Scan) {
			out.mu.Lock()
			defer out.mu.Unlock()
			out.scans = append(out.scans, s.Status)
		},
		Finding: func(f api.Finding) {
			out.mu.Lock()
			defer out.mu.Unlock()
			out.findings = append(out.findings, f.ID)
		},
		Reconnecting: func(in time.Duration, _ error) {
			out.mu.Lock()
			defer out.mu.Unlock()
			out.retries = append(out.retries, in)
		},
		Sleep: noSleep,
	}
	if change != nil {
		change(&o)
	}
	go func() {
		defer close(out.done)
		out.scan, out.err = follow.Run(ctx, o)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-out.done:
		case <-time.After(wait):
			t.Error("Run didn't return after cancel")
		}
	})
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitJoins(t *testing.T, srv *channeltest.Server, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	if err := srv.WaitJoins(ctx, n); err != nil {
		t.Fatal(err)
	}
}

func push(t *testing.T, srv *channeltest.Server, event string, payload any) {
	t.Helper()
	if err := srv.Push(event, payload); err != nil {
		t.Fatal(err)
	}
}

// The backlog over REST, then the gap closed once after the join, then
// live steps, each once, until ended.
func TestBacklogThenLiveUntilEnded(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: token})
	r := &rest{scan: running(),
		steps: map[string][]api.Step{"": {step("a"), step("b")}, "c1": {step("b"), step("c")}},
		next:  map[string]string{"": "c1", "c1": "c2"}}
	out := start(t, srv.URL, r, nil)
	waitJoins(t, srv, 1)
	eventually(t, "the gap fill", func() bool { return len(out.Steps()) == 3 })
	push(t, srv, "step", stepPush("c")) // Already shown by the gap fill.
	push(t, srv, "step", stepPush("d"))
	push(t, srv, "scan", map[string]any{"status": "running", "winding_down": true})
	push(t, srv, "finding", map[string]any{"id": "FndHigh7Kq2X", "severity": "high"})
	push(t, srv, "ended", map[string]any{"scan": map[string]any{"id": "RunScn7Kq2Xp", "number": 1, "status": "completed"}})
	scan, err := out.result()
	if err != nil || scan == nil || scan.Status != "completed" {
		t.Fatalf("Run = %+v, %v", scan, err)
	}
	if got := out.Steps(); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("steps = %v", got)
	}
	if !slices.Equal(out.scans, []string{"running"}) || !slices.Equal(out.findings, []string{"FndHigh7Kq2X"}) {
		t.Errorf("scans %v, findings %v", out.scans, out.findings)
	}
	if got := srv.Frames(); len(got) == 0 || got[0].Topic != topic {
		t.Errorf("joined %v", got)
	}
}

// A scan already over: its steps, and no socket at all.
func TestEndedScanNeedsNoSocket(t *testing.T) {
	noLeaks(t)
	r := &rest{scan: api.Scan{ID: "RunScn7Kq2Xp", Number: 2, Status: "completed"},
		steps: map[string][]api.Step{"": {step("a")}}, next: map[string]string{"": ""}}
	out := start(t, "http://127.0.0.1:1", r, nil)
	scan, err := out.result()
	if err != nil || scan.Status != "completed" || !slices.Equal(out.Steps(), []string{"a"}) {
		t.Fatalf("Run = %+v, %v; steps %v", scan, err, out.Steps())
	}
}

// A dropped socket reconnects and fills the gap from the last cursor.
func TestDropReconnectsAndFillsTheGap(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: token})
	r := &rest{scan: running(),
		steps: map[string][]api.Step{"": {step("a")}}, next: map[string]string{"": "c1"}}
	out := start(t, srv.URL, r, nil)
	waitJoins(t, srv, 1)
	r.mu.Lock()
	r.steps["c1"] = []api.Step{step("b")}
	r.next["c1"] = "c2"
	r.mu.Unlock()
	srv.Drop()
	waitJoins(t, srv, 2)
	eventually(t, "the gap fill", func() bool { return len(out.Steps()) == 2 })
	push(t, srv, "ended", map[string]any{"scan": map[string]any{"status": "failed"}})
	scan, err := out.result()
	if err != nil || scan.Status != "failed" {
		t.Fatalf("Run = %+v, %v", scan, err)
	}
	if got := out.Steps(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("steps = %v", got)
	}
	if len(out.retries) != 1 || out.retries[0] != time.Second {
		t.Errorf("retries = %v", out.retries)
	}
}

// Losing access ends it: unauthorized as revoked, not_found as such.
func TestAccessLostEnds(t *testing.T) {
	for code, want := range map[string]error{"unauthorized": channel.ErrRevoked, "not_found": api.ErrNotFound} {
		t.Run(code, func(t *testing.T) {
			noLeaks(t)
			srv := channeltest.New(t, channeltest.Options{Token: token})
			r := &rest{scan: running(), next: map[string]string{"": "c1"}}
			out := start(t, srv.URL, r, nil)
			waitJoins(t, srv, 1)
			push(t, srv, "access_lost", map[string]any{"error": map[string]any{"code": code}})
			if _, err := out.result(); !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
		})
	}
}

// A refused join ends it with the server's error.
func TestJoinRefusedEnds(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: token, Join: func(string, json.RawMessage) channeltest.Reply {
		return channeltest.Reply{Status: "error", Response: map[string]any{"error": map[string]any{"code": "not_found", "message": "Gone."}}}
	}})
	out := start(t, srv.URL, &rest{scan: running(), next: map[string]string{"": "c1"}}, nil)
	var je *channel.JoinError
	if _, err := out.result(); !errors.As(err, &je) || je.Code != "not_found" {
		t.Fatalf("err = %v", err)
	}
}

// A socket that never opens is given up after five tries.
func TestGivesUpWhenTheStreamNeverOpens(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	srv.Close()
	out := start(t, srv.URL, &rest{scan: running(), next: map[string]string{"": "c1"}}, nil)
	if _, err := out.result(); !errors.Is(err, follow.ErrNeverOpened) {
		t.Fatalf("err = %v", err)
	}
	if want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}; !slices.Equal(out.retries, want) {
		t.Errorf("retries = %v, want %v", out.retries, want)
	}
}

// A refused upgrade asks REST whether the token still works.
func TestRefusedConnectionProbesREST(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: "another"})
	r := &rest{scan: running(), next: map[string]string{"": "c1"}}
	out := start(t, srv.URL, r, func(o *follow.Options) {
		o.Sleep = func(ctx context.Context, _ time.Duration) error {
			r.mu.Lock()
			r.scanErr = &api.Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "revoked"}
			r.mu.Unlock()
			return ctx.Err()
		}
	})
	if _, err := out.result(); !errors.Is(err, api.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

// Passing REST failures retry; others end it.
func TestRESTErrors(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: token})
	r := &rest{scan: running(), next: map[string]string{"": "c1"},
		stepsErr: []error{&api.Error{Status: http.StatusBadGateway, Code: "http_error"}}}
	out := start(t, srv.URL, r, nil)
	waitJoins(t, srv, 1)
	push(t, srv, "ended", map[string]any{"scan": map[string]any{"status": "completed"}})
	if _, err := out.result(); err != nil {
		t.Fatalf("err = %v", err)
	}

	r = &rest{scan: running(), stepsErr: []error{&api.Error{Status: http.StatusNotFound, Code: "not_found"}}}
	out = start(t, srv.URL, r, nil)
	if _, err := out.result(); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// Ctrl-C stops it at once, from a backoff too.
func TestStopDuringBackoff(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	srv.Close()
	out := start(t, srv.URL, &rest{scan: running(), next: map[string]string{"": "c1"}}, func(o *follow.Options) {
		o.Sleep = func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
	})
	time.Sleep(50 * time.Millisecond)
	out.cancel()
	if _, err := out.result(); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// Terms the person hasn't accepted end it too, saying so.
func TestAccessLostToTermsSaysSo(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: token})
	out := start(t, srv.URL, &rest{scan: running(), next: map[string]string{"": "c1"}}, nil)
	waitJoins(t, srv, 1)
	push(t, srv, "access_lost", map[string]any{"error": map[string]any{"code": "terms_not_accepted"}})
	_, err := out.result()
	var lost *follow.AccessLostError
	if !errors.As(err, &lost) || lost.Code != "terms_not_accepted" {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, api.ErrNotFound) || !strings.Contains(err.Error(), "terms") {
		t.Errorf("err = %q, want it about the terms, not the scan's reach", err)
	}
}
