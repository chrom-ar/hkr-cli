// Package apitest is a fake HKr for tests, built from recorded
// fixtures of the real API's answers (testdata/fixtures, one file per
// endpoint and case). The same files are rendered by HKr's real
// controllers in its own tests, so the fake cannot drift from the server
// unnoticed (make fixtures-check).
package apitest

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync"
)

//go:embed testdata/fixtures/*.json
var files embed.FS

// Fixture is one recorded request and its answer.
type Fixture struct {
	Name    string `json:"-"`
	Request struct {
		Method string            `json:"method"`
		Path   string            `json:"path"`
		Query  map[string]string `json:"query"`
		Body   json.RawMessage   `json:"body"`
		// Headers are request headers that must match (Accept).
		Headers map[string]string `json:"headers"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		// Body is the JSON answer, or a JSON string holding the raw
		// answer when Content-Type is not JSON (Markdown).
		Body json.RawMessage `json:"body"`
	} `json:"response"`
}

// Names lists every fixture, sorted.
func Names() []string {
	entries, _ := files.ReadDir("testdata/fixtures")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	return names
}

// Load reads the fixture called name ("me.ok").
func Load(t T, name string) Fixture {
	t.Helper()
	b, err := files.ReadFile(path.Join("testdata/fixtures", name+".json"))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	var f Fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	f.Name = name
	return f
}

// T is the part of testing.TB the server needs, so scripts can use it
// too.
type T interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Cleanup(func())
}

// Server answers requests with the fixtures it was given. When several
// match a request they are served in order, and the last one repeats
// (a device flow's pending, pending, approved). A request no fixture
// matches fails the test.
type Server struct {
	*httptest.Server
	t        T
	mu       sync.Mutex
	fixtures []Fixture
	served   map[int]bool
	requests []Request
	rewrites map[string]string
}

// Request is a request the server received.
type Request struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

// NewServer starts a server answering with the named fixtures. It stops
// when the test ends.
func NewServer(t T, names ...string) *Server {
	t.Helper()
	s := &Server{t: t, served: map[int]bool{}}
	for _, n := range names {
		s.fixtures = append(s.fixtures, Load(t, n))
	}
	// Unstarted, so s.Server is set before any request can read its URL.
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	s.Start()
	t.Cleanup(s.Close)
	return s
}

// StorageOrigin is where recorded evidence downloads redirect to.
const StorageOrigin = "https://mock.s3.test"

// RewriteOrigin makes answers point at to wherever they point at from (a
// storage host, in redirects).
func (s *Server) RewriteOrigin(from, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rewrites == nil {
		s.rewrites = map[string]string{}
	}
	s.rewrites[from] = to
}

// rewrite points recorded origins at the fake servers.
func (s *Server) rewrite(v string) string {
	v = strings.ReplaceAll(v, recordedOrigin, s.URL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for from, to := range s.rewrites {
		v = strings.ReplaceAll(v, from, to)
	}
	return v
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// recordedOrigin is the HKr the fixtures were recorded on; answers
// (URLs in bodies, Location headers) point at the fake server instead.
const recordedOrigin = "http://hkr.test"

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Header: r.Header.Clone(), Body: body,
	})
	f, ok := s.match(r, body)
	s.mu.Unlock()

	if !ok {
		s.t.Errorf("apitest: no fixture for %s %s", r.Method, r.URL.RequestURI())
		http.Error(w, "no fixture", http.StatusNotImplemented)
		return
	}
	for k, v := range f.Response.Headers {
		w.Header().Set(k, s.rewrite(v))
	}
	out := []byte(s.rewrite(string(f.Response.Body)))
	if ct := w.Header().Get("Content-Type"); ct == "" {
		if len(out) > 0 {
			w.Header().Set("Content-Type", "application/json")
		}
	} else if !strings.HasPrefix(ct, "application/json") {
		var raw string
		if err := json.Unmarshal(out, &raw); err != nil {
			s.t.Errorf("apitest: %s: a %s body must be a JSON string", f.Name, ct)
		}
		out = []byte(raw)
	}
	w.WriteHeader(f.Response.Status)
	if f.Response.Status != http.StatusNoContent && len(out) > 0 && string(out) != "null" {
		_, _ = w.Write(out)
	}
}

// match picks the first unserved matching fixture, else the last match.
func (s *Server) match(r *http.Request, body []byte) (Fixture, bool) {
	last := -1
	for i, f := range s.fixtures {
		if !matches(f, r, body) {
			continue
		}
		if !s.served[i] {
			s.served[i] = true
			return f, true
		}
		last = i
	}
	if last < 0 {
		return Fixture{}, false
	}
	return s.fixtures[last], true
}

func matches(f Fixture, r *http.Request, body []byte) bool {
	if f.Request.Method != r.Method || f.Request.Path != r.URL.Path {
		return false
	}
	q := r.URL.Query()
	if len(q) != len(f.Request.Query) {
		return false
	}
	for k, v := range f.Request.Query {
		if q.Get(k) != v {
			return false
		}
	}
	for k, v := range f.Request.Headers {
		if r.Header.Get(k) != v {
			return false
		}
	}
	if len(f.Request.Body) == 0 || string(f.Request.Body) == "null" || string(f.Request.Body) == `""` {
		return true
	}
	return jsonEqual(f.Request.Body, body)
}

func jsonEqual(a, b []byte) bool {
	var xv, yv any
	if json.Unmarshal(a, &xv) != nil || json.Unmarshal(b, &yv) != nil {
		return false
	}
	xb, _ := json.Marshal(xv) // Maps marshal with sorted keys.
	yb, _ := json.Marshal(yv)
	return bytes.Equal(xb, yb)
}
