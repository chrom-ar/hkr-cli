// Package channel is a Phoenix Channels client (v2 JSON serializer) over
// WebSocket, for following a scan live (SPEC 4.4).
//
// A Conn is one socket with at most one joined channel. It ends once, for
// one reason, which Err reports: ErrClosed after Close, the context's error
// when the Dial context ends, an error matching ErrRevoked when HKr
// closes the socket with 4001 or 4003, a *JoinError when the join is
// refused, and an error matching ErrDropped otherwise (the socket dropped, a
// heartbeat went unanswered, phx_close or phx_error). Reconnecting is the
// caller's job.
package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Defaults for the Options left zero.
const (
	DefaultConnectTimeout    = 10 * time.Second
	DefaultJoinTimeout       = 10 * time.Second
	DefaultHeartbeatInterval = 30 * time.Second
)

const (
	// writeTimeout bounds every frame sent.
	writeTimeout = 10 * time.Second
	// readLimit bounds one frame read.
	readLimit = 32 << 20
	// heartbeatTopic is Phoenix's socket-level topic.
	heartbeatTopic = "phoenix"
)

var (
	// ErrDropped means the connection was lost, or never opened, and trying
	// again may work.
	ErrDropped = errors.New("the live stream dropped")
	// ErrRevoked means HKr ended the stream because the token was
	// revoked or lost access.
	ErrRevoked = errors.New("hkr closed the stream: the token was revoked or lost access")
	// ErrClosed means Close was called.
	ErrClosed = errors.New("the live stream was closed")
)

// RefusedError is a WebSocket upgrade the server answered with an HTTP
// status instead of 101, as HKr does (403) for a bad token. It also
// matches ErrDropped: the status alone doesn't tell a revoked token from a
// server without the socket, so ask the REST API.
type RefusedError struct {
	StatusCode int
}

func (e *RefusedError) Error() string {
	return "hkr refused the live stream: HTTP " + strconv.Itoa(e.StatusCode)
}

// Is makes a RefusedError match ErrDropped.
func (e *RefusedError) Is(target error) bool { return target == ErrDropped }

// JoinError is a refused join: {"status": "error", "response": {"error":
// {"code", "message"}}}. Code is "" when the server gave only a reason.
// Message is the server's text, untrusted: neutralize it before printing.
//
// The codes forbidden, unauthorized and read_only match ErrRevoked;
// unavailable matches ErrDropped.
type JoinError struct {
	Code    string
	Message string
}

func (e *JoinError) Error() string {
	if e.Code == "" {
		return "the live stream refused to join: " + e.Message
	}
	return "the live stream refused to join (" + e.Code + "): " + e.Message
}

// Is makes a JoinError match ErrRevoked or ErrDropped by its code.
func (e *JoinError) Is(target error) bool {
	switch e.Code {
	case "forbidden", "unauthorized", "read_only":
		return target == ErrRevoked
	case "unavailable":
		return target == ErrDropped
	}
	return false
}

// Push is an event the server pushed on the joined channel, other than
// Phoenix's own (phx_reply, phx_close, phx_error).
type Push struct {
	Event   string
	Payload json.RawMessage
}

// Options configure Dial.
type Options struct {
	// URL is HKr's origin, http(s)://host[/prefix].
	URL string
	// Token is the personal token. It goes only in Sec-WebSocket-Protocol,
	// never in the URL, an error or a log.
	Token string
	// UserAgent is sent with the upgrade request when set.
	UserAgent string
	// HTTPClient makes the upgrade request; nil uses http.DefaultClient.
	HTTPClient *http.Client
	// ConnectTimeout bounds the upgrade; zero is DefaultConnectTimeout.
	ConnectTimeout time.Duration
	// JoinTimeout bounds waiting for the join's reply; zero is
	// DefaultJoinTimeout.
	JoinTimeout time.Duration
	// HeartbeatInterval is how often to send a heartbeat; a heartbeat not
	// answered by the next one means the connection dropped. Zero is
	// DefaultHeartbeatInterval.
	HeartbeatInterval time.Duration
	// OnPush receives every push on the joined channel, in order, on the
	// connection's read goroutine: it must return quickly, or heartbeat
	// replies wait behind it.
	OnPush func(Push)
}

// Conn is a Phoenix socket. Its methods are safe for concurrent use.
type Conn struct {
	ws          *websocket.Conn
	ctx         context.Context
	cancel      context.CancelFunc
	joinTimeout time.Duration
	heartbeat   time.Duration
	onPush      func(Push)
	done        chan struct{}

	mu       sync.Mutex
	ref      uint64
	topic    string
	joinRef  string
	waiters  map[string]chan reply
	heartRef string // the unanswered heartbeat's ref, or ""
	joined   json.RawMessage
	err      error
}

type reply struct {
	Status   string          `json:"status"`
	Response json.RawMessage `json:"response"`
}

// Dial opens a socket to HKr. ctx bounds the connection's whole life:
// when it ends, the connection ends as if closed, with ctx's error.
//
// A failure to connect matches ErrDropped; an upgrade answered with an HTTP
// status is also a *RefusedError.
func Dial(ctx context.Context, opts Options) (*Conn, error) {
	u, err := SocketURL(opts.URL)
	if err != nil {
		return nil, err
	}
	connectTimeout := or(opts.ConnectTimeout, DefaultConnectTimeout)
	dctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	header := http.Header{}
	if opts.UserAgent != "" {
		header.Set("User-Agent", opts.UserAgent)
	}
	ws, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{
		HTTPClient: opts.HTTPClient,
		HTTPHeader: header,
		// The library joins these with ",": the leading space makes the
		// header "phoenix, base64url.bearer.phx.…", as Phoenix's own
		// client sends it, while "phoenix" stays the one the server may
		// pick. The token is standard base64 without padding; "base64url"
		// is only Phoenix's label.
		Subprotocols: []string{"phoenix", " base64url.bearer.phx." + base64.RawStdEncoding.EncodeToString([]byte(opts.Token))},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("channel: connect: %w", ctx.Err())
		}
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return nil, fmt.Errorf("channel: connect: %w", &RefusedError{StatusCode: resp.StatusCode})
		}
		if dctx.Err() != nil {
			return nil, fmt.Errorf("channel: connect: %w: no answer in %s", ErrDropped, connectTimeout)
		}
		return nil, fmt.Errorf("channel: connect: %w: %w", ErrDropped, err)
	}
	ws.SetReadLimit(readLimit)

	c := &Conn{
		ws:          ws,
		joinTimeout: or(opts.JoinTimeout, DefaultJoinTimeout),
		heartbeat:   or(opts.HeartbeatInterval, DefaultHeartbeatInterval),
		onPush:      opts.OnPush,
		done:        make(chan struct{}),
		waiters:     map[string]chan reply{},
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	go c.run()
	return c, nil
}

// Join joins topic with payload as the join's parameters (nil sends {}),
// and waits for the reply, at most the join timeout. A Conn joins once. If
// Join fails, the connection ends with the same error: a *JoinError when
// refused, ErrDropped when unanswered, or ctx's error.
func (c *Conn) Join(ctx context.Context, topic string, payload map[string]string) error {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	if c.topic != "" {
		c.mu.Unlock()
		return errors.New("channel: already joined")
	}
	ref := c.nextRef()
	c.topic, c.joinRef = topic, ref
	replies := make(chan reply, 1)
	c.waiters[ref] = replies
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiters, ref)
		c.mu.Unlock()
	}()

	if payload == nil {
		payload = map[string]string{}
	}
	if err := c.send(&ref, ref, topic, "phx_join", payload); err != nil {
		return c.fail(err)
	}
	timer := time.NewTimer(c.joinTimeout)
	defer timer.Stop()
	select {
	case r := <-replies:
		if r.Status == "ok" {
			c.mu.Lock()
			c.joined = r.Response
			c.mu.Unlock()
			return nil
		}
		return c.fail(joinError(r.Response))
	case <-timer.C:
		return c.fail(fmt.Errorf("channel: join: %w: no answer in %s", ErrDropped, c.joinTimeout))
	case <-ctx.Done():
		return c.fail(fmt.Errorf("channel: join: %w", ctx.Err()))
	case <-c.ctx.Done():
		return c.Err()
	}
}

// Joined is the response to the join, once it succeeded.
func (c *Conn) Joined() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joined
}

// Done is closed once the connection has ended and its goroutines have
// stopped.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err says why the connection ended, or nil while it's open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection at once, without a closing handshake, and
// waits for its goroutines to stop. Calling it again does nothing.
func (c *Conn) Close() error {
	c.end(ErrClosed)
	<-c.done
	return nil
}

// run reads frames until the connection ends, with the heartbeat beside it.
func (c *Conn) run() {
	defer close(c.done)
	beating := make(chan struct{})
	go func() {
		defer close(beating)
		c.beat()
	}()
	c.end(c.read())
	<-beating
}

func (c *Conn) read() error {
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			if c.ctx.Err() != nil {
				return fmt.Errorf("channel: %w", c.ctx.Err())
			}
			switch code := websocket.CloseStatus(err); code {
			case 4001, 4003:
				return fmt.Errorf("channel: closed with code %d: %w", code, ErrRevoked)
			}
			return fmt.Errorf("channel: %w: %w", ErrDropped, err)
		}
		if typ != websocket.MessageText {
			continue
		}
		if err := c.dispatch(data); err != nil {
			return err
		}
	}
}

// dispatch handles one frame; an error ends the connection.
func (c *Conn) dispatch(data []byte) error {
	var parts []json.RawMessage
	if json.Unmarshal(data, &parts) != nil || len(parts) != 5 {
		return nil
	}
	var joinRef, ref *string
	var topic, event string
	for i, target := range []any{&joinRef, &ref, &topic, &event} {
		if json.Unmarshal(parts[i], target) != nil {
			return nil
		}
	}
	payload := parts[4]

	c.mu.Lock()
	ours := c.topic != "" && topic == c.topic && (joinRef == nil || *joinRef == c.joinRef)
	c.mu.Unlock()

	switch event {
	case "phx_reply":
		if ref == nil {
			return nil
		}
		c.mu.Lock()
		if topic == heartbeatTopic && *ref == c.heartRef {
			c.heartRef = ""
		}
		w := c.waiters[*ref]
		delete(c.waiters, *ref)
		c.mu.Unlock()
		var r reply
		if w != nil && json.Unmarshal(payload, &r) == nil {
			w <- r
		}
	case "phx_close", "phx_error":
		if ours {
			return fmt.Errorf("channel: %w: the server sent %s", ErrDropped, event)
		}
	default:
		if ours && c.onPush != nil {
			c.onPush(Push{Event: event, Payload: payload})
		}
	}
	return nil
}

// beat sends a heartbeat every interval until the connection ends, and ends
// it when the previous heartbeat went unanswered.
func (c *Conn) beat() {
	ticker := time.NewTicker(c.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
		}
		c.mu.Lock()
		if c.heartRef != "" {
			c.mu.Unlock()
			c.end(fmt.Errorf("channel: %w: heartbeat not answered in %s", ErrDropped, c.heartbeat))
			return
		}
		ref := c.nextRef()
		c.heartRef = ref
		c.mu.Unlock()
		if err := c.send(nil, ref, heartbeatTopic, "heartbeat", map[string]string{}); err != nil {
			c.end(err)
			return
		}
	}
}

func (c *Conn) send(joinRef *string, ref, topic, event string, payload any) error {
	data, err := json.Marshal([]any{joinRef, ref, topic, event, payload})
	if err != nil {
		return fmt.Errorf("channel: encode %s: %w", event, err)
	}
	ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("channel: send %s: %w: %w", event, ErrDropped, err)
	}
	return nil
}

// end ends the connection with err, unless it has ended already.
func (c *Conn) end(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	c.mu.Unlock()
	c.cancel()
	_ = c.ws.CloseNow()
}

// fail ends the connection with err and returns why it ended, err unless
// it had ended already.
func (c *Conn) fail(err error) error {
	c.end(err)
	return c.Err()
}

// nextRef returns a new ref; c.mu must be held.
func (c *Conn) nextRef() string {
	c.ref++
	return strconv.FormatUint(c.ref, 10)
}

func joinError(response json.RawMessage) error {
	var r struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(response, &r)
	e := &JoinError{Code: r.Error.Code, Message: r.Error.Message}
	if e.Code == "" && e.Message == "" {
		e.Message = r.Reason
	}
	return e
}

// SocketURL is the socket's URL for HKr's origin:
// https://host[/prefix] → wss://host[/prefix]/api/v1/socket/websocket?vsn=2.0.0.
func SocketURL(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return "", fmt.Errorf("channel: HKr's URL: %w", err)
	}
	switch u.Scheme {
	case "http", "ws":
		u.Scheme = "ws"
	case "https", "wss":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("channel: HKr's URL %q isn't http(s)", origin)
	}
	if u.Host == "" {
		return "", fmt.Errorf("channel: HKr's URL %q has no host", origin)
	}
	u.User = nil
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/v1/socket/websocket"
	u.RawPath = ""
	u.RawQuery = "vsn=2.0.0"
	u.Fragment = ""
	return u.String(), nil
}

func or(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
