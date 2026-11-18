// Package channeltest is a fake Phoenix socket server (v2 JSON serializer)
// on a random local port, for testing the channel client and what is built
// on it.
package channeltest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// writeTimeout bounds every frame the server sends.
const writeTimeout = 5 * time.Second

// bearerLabel marks the token among the subprotocols, as Phoenix does.
const bearerLabel = "base64url.bearer.phx." //nolint:gosec // A label, not a credential.

// Frame is one message the server received: [join_ref, ref, topic, event, payload].
type Frame struct {
	JoinRef *string
	Ref     *string
	Topic   string
	Event   string
	Payload json.RawMessage
}

// Reply is how the server answers a join.
type Reply struct {
	// Status is "ok" or "error".
	Status string
	// Response is the reply's response; nil sends {}.
	Response any
	// Silent leaves the join unanswered.
	Silent bool
}

// Options configure a Server. The zero value accepts any token, answers
// every join ok and answers every heartbeat.
type Options struct {
	// Token, when set, is the only token accepted; any other is refused at
	// the upgrade with 403, as HKr does.
	Token string
	// Join answers a join; nil answers ok.
	Join func(topic string, payload json.RawMessage) Reply
	// SilentHeartbeats leaves heartbeats unanswered.
	SilentHeartbeats bool
	// REST, when set, answers every request that isn't to the socket, so
	// one server can play a whole HKr.
	REST http.Handler
}

// Connection is one upgrade request the server received.
type Connection struct {
	// Path is the request URI, query included.
	Path string
	// Protocol is the Sec-WebSocket-Protocol header as sent.
	Protocol string
	// Protocols is that header split on commas, each trimmed.
	Protocols []string
	// UserAgent is the User-Agent header.
	UserAgent string
	// Refused tells whether the upgrade was refused.
	Refused bool
}

type join struct {
	topic   string
	joinRef *string
	ws      *websocket.Conn
}

// Server is a fake Phoenix socket server. Its methods are safe for
// concurrent use.
type Server struct {
	// URL is the server's origin, http://127.0.0.1:port.
	URL string

	opts Options
	hs   *httptest.Server

	mu          sync.Mutex
	frames      []Frame
	connections []Connection
	sockets     []*websocket.Conn
	joins       []join
	joined      chan struct{} // closed and replaced on every successful join
	closed      bool
}

// New starts a Server and closes it when the test ends.
func New(t testing.TB, opts Options) *Server {
	t.Helper()
	s := &Server{opts: opts, joined: make(chan struct{})}
	s.hs = httptest.NewServer(http.HandlerFunc(s.serve))
	s.URL = s.hs.URL
	t.Cleanup(s.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if s.opts.REST != nil && !strings.HasSuffix(r.URL.Path, "/socket/websocket") {
		s.opts.REST.ServeHTTP(w, r)
		return
	}
	header := strings.Join(r.Header.Values("Sec-WebSocket-Protocol"), ",")
	var protocols []string
	for p := range strings.SplitSeq(header, ",") {
		if p = strings.TrimSpace(p); p != "" {
			protocols = append(protocols, p)
		}
	}
	conn := Connection{Path: r.RequestURI, Protocol: header, Protocols: protocols, UserAgent: r.UserAgent()}
	if s.opts.Token != "" && tokenOf(protocols) != s.opts.Token {
		conn.Refused = true
		s.record(func() { s.connections = append(s.connections, conn) })
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.record(func() { s.connections = append(s.connections, conn) })

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"phoenix"}})
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ws.CloseNow()
		return
	}
	s.sockets = append(s.sockets, ws)
	s.mu.Unlock()
	defer func() { _ = ws.CloseNow() }()

	// The request's context ends with the handler; CloseNow ends the read.
	ctx := context.WithoutCancel(r.Context())
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		f, err := decode(data)
		if err != nil {
			continue
		}
		s.record(func() { s.frames = append(s.frames, f) })
		s.answer(ws, f)
	}
}

func (s *Server) answer(ws *websocket.Conn, f Frame) {
	switch f.Event {
	case "heartbeat":
		if !s.opts.SilentHeartbeats {
			_ = send(ws, nil, f.Ref, "phoenix", "phx_reply", map[string]any{"status": "ok", "response": map[string]any{}})
		}
	case "phx_join":
		reply := Reply{Status: "ok"}
		if s.opts.Join != nil {
			reply = s.opts.Join(f.Topic, f.Payload)
		}
		if reply.Silent {
			return
		}
		response := reply.Response
		if response == nil {
			response = map[string]any{}
		}
		_ = send(ws, f.JoinRef, f.Ref, f.Topic, "phx_reply", map[string]any{"status": reply.Status, "response": response})
		if reply.Status == "ok" {
			s.mu.Lock()
			s.joins = append(s.joins, join{topic: f.Topic, joinRef: f.JoinRef, ws: ws})
			close(s.joined)
			s.joined = make(chan struct{})
			s.mu.Unlock()
		}
	}
}

func (s *Server) record(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

// Frames returns every frame received so far, in order.
func (s *Server) Frames() []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Frame(nil), s.frames...)
}

// Count returns how many frames with the event were received.
func (s *Server) Count(event string) int {
	n := 0
	for _, f := range s.Frames() {
		if f.Event == event {
			n++
		}
	}
	return n
}

// Connections returns every upgrade request received so far, refused ones
// included.
func (s *Server) Connections() []Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Connection(nil), s.connections...)
}

// Joins returns how many joins have succeeded.
func (s *Server) Joins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.joins)
}

// WaitJoins waits until n joins have succeeded.
func (s *Server) WaitJoins(ctx context.Context, n int) error {
	for {
		s.mu.Lock()
		count, joined := len(s.joins), s.joined
		s.mu.Unlock()
		if count >= n {
			return nil
		}
		select {
		case <-joined:
		case <-ctx.Done():
			return fmt.Errorf("channeltest: waiting for %d joins, got %d: %w", n, count, ctx.Err())
		}
	}
}

func (s *Server) last() (join, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.joins) == 0 {
		return join{}, errors.New("channeltest: nobody has joined")
	}
	return s.joins[len(s.joins)-1], nil
}

// Push sends an event to the latest joined channel.
func (s *Server) Push(event string, payload any) error {
	j, err := s.last()
	if err != nil {
		return err
	}
	return send(j.ws, nil, nil, j.topic, event, payload)
}

// PhxClose sends phx_close to the latest joined channel.
func (s *Server) PhxClose() error {
	j, err := s.last()
	if err != nil {
		return err
	}
	return send(j.ws, j.joinRef, nil, j.topic, "phx_close", map[string]any{})
}

// PhxError sends phx_error to the latest joined channel.
func (s *Server) PhxError() error {
	j, err := s.last()
	if err != nil {
		return err
	}
	return send(j.ws, j.joinRef, nil, j.topic, "phx_error", map[string]any{})
}

// Drop closes every open socket abruptly, without a close frame.
func (s *Server) Drop() {
	for _, ws := range s.takeSockets() {
		_ = ws.CloseNow()
	}
}

// CloseWith closes every open socket with a close code, as HKr does
// with 4001 or 4003 when access is revoked.
func (s *Server) CloseWith(code int, reason string) {
	for _, ws := range s.takeSockets() {
		_ = ws.Close(websocket.StatusCode(code), reason)
	}
}

func (s *Server) takeSockets() []*websocket.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	sockets := s.sockets
	s.sockets = nil
	return sockets
}

// Close drops every socket and stops the server. Calling it again does
// nothing.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.Drop()
	s.hs.Close()
}

func tokenOf(protocols []string) string {
	for _, p := range protocols {
		if encoded, ok := strings.CutPrefix(p, bearerLabel); ok {
			token, err := base64.RawStdEncoding.DecodeString(encoded)
			if err != nil {
				return ""
			}
			return string(token)
		}
	}
	return ""
}

func decode(data []byte) (Frame, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(data, &parts); err != nil {
		return Frame{}, fmt.Errorf("channeltest: decode frame: %w", err)
	}
	if len(parts) != 5 {
		return Frame{}, fmt.Errorf("channeltest: frame has %d parts, not 5", len(parts))
	}
	var f Frame
	for i, target := range []any{&f.JoinRef, &f.Ref, &f.Topic, &f.Event} {
		if err := json.Unmarshal(parts[i], target); err != nil {
			return Frame{}, fmt.Errorf("channeltest: decode frame: %w", err)
		}
	}
	f.Payload = parts[4]
	return f, nil
}

func send(ws *websocket.Conn, joinRef, ref *string, topic, event string, payload any) error {
	data, err := json.Marshal([]any{joinRef, ref, topic, event, payload})
	if err != nil {
		return fmt.Errorf("channeltest: encode frame: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("channeltest: send %s: %w", event, err)
	}
	return nil
}
