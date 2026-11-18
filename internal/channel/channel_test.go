package channel_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/channel"
	"github.com/chrom-ar/hkr-cli/internal/channel/channeltest"
)

// Its standard base64 has "+" and "/" and padding: what base64url, or
// keeping the padding, would get wrong.
const token = "cltp_>>?~~" //nolint:gosec // A fake token.

const topic = "logs:acme/shop"

// wait bounds every wait in these tests.
const wait = 5 * time.Second

// noLeaks fails the test if goroutines started during it outlive it. Call
// it first, so its check runs after every other cleanup.
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

// client is an HTTP client of the test's own, so no idle connection
// outlives it.
func client(t *testing.T) *http.Client {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr}
}

type opened struct {
	conn    *channel.Conn
	records chan channel.Push
}

func options(t *testing.T, url string) (channel.Options, chan channel.Push) {
	t.Helper()
	records := make(chan channel.Push, 100)
	return channel.Options{
		URL:               url,
		Token:             token,
		UserAgent:         "hkr-test",
		HTTPClient:        client(t),
		ConnectTimeout:    wait,
		JoinTimeout:       wait,
		HeartbeatInterval: time.Minute,
		OnPush:            func(p channel.Push) { records <- p },
	}, records
}

// open dials and joins topic with the level=warning filter.
func open(t *testing.T, url string, change func(*channel.Options)) opened {
	t.Helper()
	opts, records := options(t, url)
	if change != nil {
		change(&opts)
	}
	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()
	conn, err := channel.Dial(t.Context(), opts)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Join(ctx, topic, map[string]string{"level": "warning"}); err != nil {
		t.Fatalf("Join: %v", err)
	}
	return opened{conn: conn, records: records}
}

// ended waits for the connection to end and returns why.
func ended(t *testing.T, conn *channel.Conn) error {
	t.Helper()
	select {
	case <-conn.Done():
		return conn.Err()
	case <-time.After(wait):
		t.Fatal("the connection didn't end")
		return nil
	}
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

func encoded() string {
	return base64.RawStdEncoding.EncodeToString([]byte(token))
}

// channel.test.ts: "joins with the token in the subprotocol (standard
// base64, no padding), never the URL".
func TestDialSendsTokenInSubprotocolNeverURL(t *testing.T) {
	noLeaks(t)
	if std := base64.StdEncoding.EncodeToString([]byte(token)); !strings.ContainsAny(std, "+/") || !strings.HasSuffix(std, "=") {
		t.Fatalf("the test token's base64 %q needs + or / and padding", std)
	}
	srv := channeltest.New(t, channeltest.Options{Token: token})
	o := open(t, srv.URL, nil)

	conns := srv.Connections()
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	c := conns[0]
	if c.Path != "/api/v1/socket/websocket?vsn=2.0.0" {
		t.Errorf("path = %q", c.Path)
	}
	if strings.Contains(c.Path, encoded()) || strings.Contains(c.Path, "cltp_") {
		t.Errorf("the token is in the URL: %q", c.Path)
	}
	if want := "phoenix, base64url.bearer.phx." + encoded(); c.Protocol != want {
		t.Errorf("Sec-WebSocket-Protocol = %q, want %q", c.Protocol, want)
	}
	if c.UserAgent != "hkr-test" {
		t.Errorf("User-Agent = %q", c.UserAgent)
	}
	if err := o.conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// channel.test.ts: "join payload carries the filters".
func TestJoinSendsTopicAndFilters(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	open(t, srv.URL, nil)

	for _, f := range srv.Frames() {
		if f.Event != "phx_join" {
			continue
		}
		if f.Topic != topic {
			t.Errorf("topic = %q", f.Topic)
		}
		if string(f.Payload) != `{"level":"warning"}` {
			t.Errorf("payload = %s", f.Payload)
		}
		if f.JoinRef == nil || f.Ref == nil || *f.JoinRef != *f.Ref {
			t.Errorf("join_ref %v and ref %v should be the same", f.JoinRef, f.Ref)
		}
		return
	}
	t.Fatal("no phx_join received")
}

func TestJoinWithoutFiltersSendsEmptyObject(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	opts, _ := options(t, srv.URL)
	conn, err := channel.Dial(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Join(t.Context(), topic, nil); err != nil {
		t.Fatal(err)
	}
	if f := srv.Frames()[0]; string(f.Payload) != `{}` {
		t.Errorf("payload = %s, want {}", f.Payload)
	}
}

// channel.test.ts: "refused join: forbidden → auth; not_found;
// unavailable; invalid".
func TestJoinRefused(t *testing.T) {
	for _, tc := range []struct {
		code             string
		revoked, dropped bool
	}{
		{"forbidden", true, false},
		{"unauthorized", true, false},
		{"read_only", true, false},
		{"not_found", false, false},
		{"unavailable", false, true},
		{"invalid", false, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			noLeaks(t)
			srv := channeltest.New(t, channeltest.Options{Join: func(string, json.RawMessage) channeltest.Reply {
				return channeltest.Reply{Status: "error", Response: map[string]any{"error": map[string]any{"code": tc.code, "message": "nope " + tc.code}}}
			}})
			opts, _ := options(t, srv.URL)
			conn, err := channel.Dial(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()

			err = conn.Join(t.Context(), topic, nil)
			var je *channel.JoinError
			if !errors.As(err, &je) {
				t.Fatalf("Join error = %v, want a JoinError", err)
			}
			if je.Code != tc.code || je.Message != "nope "+tc.code {
				t.Errorf("JoinError = %+v", je)
			}
			if got := errors.Is(err, channel.ErrRevoked); got != tc.revoked {
				t.Errorf("errors.Is(ErrRevoked) = %v, want %v", got, tc.revoked)
			}
			if got := errors.Is(err, channel.ErrDropped); got != tc.dropped {
				t.Errorf("errors.Is(ErrDropped) = %v, want %v", got, tc.dropped)
			}
			if !errors.As(ended(t, conn), &je) {
				t.Errorf("Err() = %v, want the JoinError", conn.Err())
			}
		})
	}
}

func TestJoinRefusedWithReasonOnly(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Join: func(string, json.RawMessage) channeltest.Reply {
		return channeltest.Reply{Status: "error", Response: map[string]any{"reason": "unmatched topic"}}
	}})
	opts, _ := options(t, srv.URL)
	conn, err := channel.Dial(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var je *channel.JoinError
	if err := conn.Join(t.Context(), topic, nil); !errors.As(err, &je) || je.Code != "" || je.Message != "unmatched topic" {
		t.Fatalf("Join error = %v (%+v)", err, je)
	}
}

func TestJoinTwiceFails(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	o := open(t, srv.URL, nil)
	if err := o.conn.Join(t.Context(), "logs:acme/other", nil); err == nil {
		t.Fatal("a second Join succeeded")
	}
}

func TestJoinUnansweredTimesOut(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Join: func(string, json.RawMessage) channeltest.Reply {
		return channeltest.Reply{Silent: true}
	}})
	opts, _ := options(t, srv.URL)
	opts.JoinTimeout = 50 * time.Millisecond
	conn, err := channel.Dial(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	if err := conn.Join(t.Context(), topic, nil); !errors.Is(err, channel.ErrDropped) {
		t.Fatalf("Join error = %v, want ErrDropped", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Join took %s", d)
	}
	if !errors.Is(ended(t, conn), channel.ErrDropped) {
		t.Errorf("Err() = %v", conn.Err())
	}
}

func TestJoinContextEnds(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Join: func(string, json.RawMessage) channeltest.Reply {
		return channeltest.Reply{Silent: true}
	}})
	opts, _ := options(t, srv.URL)
	conn, err := channel.Dial(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := conn.Join(ctx, topic, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Join error = %v, want DeadlineExceeded", err)
	}
	if err := ended(t, conn); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Err() = %v, want DeadlineExceeded", err)
	}
}

// Pushes on the joined topic reach OnPush in order, whatever the event.
func TestPushesArriveInOrder(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	o := open(t, srv.URL, nil)

	for _, p := range []struct {
		event   string
		payload any
	}{
		{"step", map[string]any{"id": "1"}},
		{"scan", map[string]any{"status": "running"}},
		{"ended", map[string]any{"scan": map[string]any{"status": "completed"}}},
	} {
		if err := srv.Push(p.event, p.payload); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{`step {"id":"1"}`, `scan {"status":"running"}`, `ended {"scan":{"status":"completed"}}`}
	for _, w := range want {
		select {
		case p := <-o.records:
			if got := p.Event + " " + string(p.Payload); got != w {
				t.Errorf("push = %s, want %s", got, w)
			}
		case <-time.After(wait):
			t.Fatal("no push")
		}
	}
	select {
	case p := <-o.records:
		t.Errorf("unexpected push %+v", p)
	default:
	}
}

func TestLargePushArrives(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	o := open(t, srv.URL, nil)
	data := make([]any, 500)
	for i := range data {
		data[i] = map[string]any{"id": i, "message": strings.Repeat("x", 1000)}
	}
	if err := srv.Push("step", map[string]any{"data": data}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-o.records:
		var r struct{ Data []any }
		if json.Unmarshal(p.Payload, &r) != nil || len(r.Data) != 500 {
			t.Errorf("got %d items", len(r.Data))
		}
	case <-time.After(wait):
		t.Fatalf("no records; connection: %v", o.conn.Err())
	}
}

// channel.test.ts: "heartbeat every heartbeatMs; no reply → closed
// resolves dropped".
func TestUnansweredHeartbeatDrops(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{SilentHeartbeats: true})
	o := open(t, srv.URL, func(o *channel.Options) { o.HeartbeatInterval = 50 * time.Millisecond })

	if err := ended(t, o.conn); !errors.Is(err, channel.ErrDropped) || errors.Is(err, channel.ErrRevoked) {
		t.Errorf("Err() = %v, want ErrDropped", err)
	}
	n := 0
	for _, f := range srv.Frames() {
		if f.Event == "heartbeat" && f.Topic == "phoenix" {
			n++
		}
	}
	if n < 1 {
		t.Errorf("got %d heartbeats on phoenix, want at least 1", n)
	}
}

// channel.test.ts: "answered heartbeats keep the channel open".
func TestAnsweredHeartbeatsKeepOpen(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	o := open(t, srv.URL, func(o *channel.Options) { o.HeartbeatInterval = 20 * time.Millisecond })

	eventually(t, "3 heartbeats", func() bool { return srv.Count("heartbeat") >= 3 })
	select {
	case <-o.conn.Done():
		t.Fatalf("the connection ended: %v", o.conn.Err())
	default:
	}
	if err := o.conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.conn.Err(); !errors.Is(err, channel.ErrClosed) {
		t.Errorf("Err() = %v, want ErrClosed", err)
	}
}

// channel.test.ts: "server drops the socket → dropped; close code 4001 →
// auth; phx_close → dropped".
func TestServerEndsTheStream(t *testing.T) {
	for _, tc := range []struct {
		name    string
		end     func(*channeltest.Server) error
		revoked bool
	}{
		{"socket dropped", func(s *channeltest.Server) error { s.Drop(); return nil }, false},
		{"close 4001", func(s *channeltest.Server) error { s.CloseWith(4001, "revoked"); return nil }, true},
		{"close 4003", func(s *channeltest.Server) error { s.CloseWith(4003, "forbidden"); return nil }, true},
		{"close 1000", func(s *channeltest.Server) error { s.CloseWith(1000, "bye"); return nil }, false},
		{"close 1011", func(s *channeltest.Server) error { s.CloseWith(1011, "crash"); return nil }, false},
		{"phx_close", (*channeltest.Server).PhxClose, false},
		{"phx_error", (*channeltest.Server).PhxError, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noLeaks(t)
			srv := channeltest.New(t, channeltest.Options{})
			o := open(t, srv.URL, nil)
			if err := tc.end(srv); err != nil {
				t.Fatal(err)
			}
			err := ended(t, o.conn)
			if errors.Is(err, channel.ErrRevoked) != tc.revoked || errors.Is(err, channel.ErrDropped) == tc.revoked {
				t.Errorf("Err() = %v; want revoked %v", err, tc.revoked)
			}
		})
	}
}

// channel.test.ts: "nothing listening: joined rejects and closed resolves
// dropped".
func TestDialNothingListening(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	url := srv.URL
	srv.Close()
	opts, _ := options(t, url)
	conn, err := channel.Dial(t.Context(), opts)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial succeeded")
	}
	var refused *channel.RefusedError
	if !errors.Is(err, channel.ErrDropped) || errors.As(err, &refused) {
		t.Errorf("Dial error = %v, want ErrDropped and no RefusedError", err)
	}
}

// channel.test.ts: "close() resolves closed and stops heartbeats".
func TestCloseStopsHeartbeats(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	o := open(t, srv.URL, func(o *channel.Options) { o.HeartbeatInterval = 20 * time.Millisecond })
	eventually(t, "a heartbeat", func() bool { return srv.Count("heartbeat") >= 1 })

	if err := o.conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ended(t, o.conn); !errors.Is(err, channel.ErrClosed) {
		t.Errorf("Err() = %v, want ErrClosed", err)
	}
	sent := len(srv.Frames())
	time.Sleep(80 * time.Millisecond)
	if n := len(srv.Frames()); n != sent {
		t.Errorf("%d frames sent after Close", n-sent)
	}
	if err := o.conn.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestContextCancelStopsEverything(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	opts, _ := options(t, srv.URL)
	opts.HeartbeatInterval = 20 * time.Millisecond
	conn, err := channel.Dial(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Join(ctx, topic, nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := ended(t, conn); !errors.Is(err, context.Canceled) {
		t.Errorf("Err() = %v, want context.Canceled", err)
	}
	sent := len(srv.Frames())
	time.Sleep(80 * time.Millisecond)
	if n := len(srv.Frames()); n != sent {
		t.Errorf("%d frames sent after cancel", n-sent)
	}
}

func TestDialRefusedUpgrade(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{Token: "cltp_someone_else"}) //nolint:gosec // A fake token.
	opts, _ := options(t, srv.URL)
	conn, err := channel.Dial(t.Context(), opts)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial succeeded")
	}
	var refused *channel.RefusedError
	if !errors.As(err, &refused) || refused.StatusCode != http.StatusForbidden {
		t.Fatalf("Dial error = %v, want a RefusedError with 403", err)
	}
	if !errors.Is(err, channel.ErrDropped) {
		t.Errorf("a refused upgrade should also be ErrDropped: %v", err)
	}
	if msg := err.Error(); strings.Contains(msg, token) || strings.Contains(msg, encoded()) {
		t.Errorf("the error shows the token: %q", msg)
	}
	if c := srv.Connections(); len(c) != 1 || !c[0].Refused {
		t.Errorf("connections = %+v", c)
	}
}

func TestDialConnectTimeout(t *testing.T) {
	noLeaks(t)
	// Accepts TCP connections and never answers.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		close(accepted)
		for c := range accepted {
			_ = c.Close()
		}
	})

	opts, _ := options(t, "http://"+ln.Addr().String())
	opts.ConnectTimeout = 50 * time.Millisecond
	start := time.Now()
	conn, err := channel.Dial(t.Context(), opts)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial succeeded")
	}
	if !errors.Is(err, channel.ErrDropped) {
		t.Errorf("Dial error = %v, want ErrDropped", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Dial took %s", d)
	}
}

func TestDialContextCanceled(t *testing.T) {
	noLeaks(t)
	srv := channeltest.New(t, channeltest.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opts, _ := options(t, srv.URL)
	if _, err := channel.Dial(ctx, opts); !errors.Is(err, context.Canceled) || errors.Is(err, channel.ErrDropped) {
		t.Errorf("Dial error = %v, want context.Canceled only", err)
	}
}

func TestDialBadURL(t *testing.T) {
	for _, url := range []string{"ftp://example.com", "example.com", "http://"} {
		opts := channel.Options{URL: url, Token: token}
		if _, err := channel.Dial(t.Context(), opts); err == nil || errors.Is(err, channel.ErrDropped) {
			t.Errorf("Dial(%q) error = %v, want a non-network error", url, err)
		}
	}
}
