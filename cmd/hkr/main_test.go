package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api/apitest"
	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain makes the test binary also act as hkr for the scripts.
// It is main's own wiring with the parts of the machine that tests must
// never touch, or that would make output vary, replaced: no keychain (so
// tokens land in a file under $WORK), no browser (the URL is logged to
// $WORK/browser.log), a fixed hostname, clock and time zone, no waiting,
// and a config directory under $WORK.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"hkr": func() {
			work := os.Getenv("WORK")
			os.Exit(run(os.Args[1:], deps{
				keyring:     noKeyring{},
				openBrowser: logBrowser(filepath.Join(work, "browser.log")),
				hostname:    func() (string, error) { return "test-host", nil },
				configDir:   func() (string, error) { return filepath.Join(work, "config"), nil },
				cacheDir:    func() (string, error) { return filepath.Join(work, "cache"), nil },
				now:         func() time.Time { return scriptNow },
				location:    time.UTC,
				sleep:       func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
			}))
		},
	})
}

// Scripts run on Wednesday, 18 November 2026, at 12:05 UTC, five
// minutes after the fixtures were recorded.
var scriptNow = time.Date(2026, 11, 18, 12, 5, 0, 0, time.UTC)

// TestScripts runs hkr through every script in testdata/script
// against a fake HKr built from the API fixtures, asserting output,
// exit codes and files.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			env.Setenv("HOME", filepath.Join(env.WorkDir, "home"))
			env.Setenv("XDG_CONFIG_HOME", "")
			env.Setenv("HKR_NO_UPDATE_NOTIFIER", "1")
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits":        exits,
			"fake-hkr":     fakeHKr,
			"fixture-body": fixtureBody,
		},
	})
}

// exits runs a program and asserts its exit code:
//
//	exits 2 hkr --bogus
//
// stdout and stderr are kept for the stdout and stderr commands.
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exits <code> <program> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exits: bad code %q", args[0])
	}
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			ts.Fatalf("exits: running %s: %v", args[1], err)
		}
		got = ee.ExitCode()
	}
	if got != want {
		ts.Fatalf("exit code = %d, want %d", got, want)
	}
}

// fixtureBody writes a fixture's response body to a file, so a script
// compares output with what HKr really sends:
//
//	fixture-body issue.get.markdown issue.md
func fixtureBody(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: fixture-body <fixture> <file>")
	}
	f := apitest.Load(&scriptT{ts: ts}, args[0])
	body := []byte(f.Response.Body)
	var s string
	if json.Unmarshal(f.Response.Body, &s) == nil {
		body = []byte(s) // A text body, as the server sends it.
	}
	if err := os.WriteFile(ts.MkAbs(args[1]), body, 0o600); err != nil {
		ts.Fatalf("fixture-body: %v", err)
	}
}

// fakeHKr starts a fake HKr answering with the named fixtures and points
// HKR_URL at it ($URL in scripts). --storage <file> also starts the
// storage evidence downloads redirect to, answering with that file:
//
//	fake-hkr me.ok orgs.list.ok
//	fake-hkr --storage request.txt finding.evidence.ok finding.evidence.file
func fakeHKr(ts *testscript.TestScript, neg bool, args []string) {
	storage := ""
	if len(args) >= 2 && args[0] == "--storage" {
		storage, args = args[1], args[2:]
	}
	if neg || len(args) == 0 {
		ts.Fatalf("usage: fake-hkr [--storage <file>] <fixture>...")
	}
	st := &scriptT{ts: ts}
	srv := apitest.NewServer(st, args...)
	if storage != "" {
		body := []byte(ts.ReadFile(storage))
		files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				st.Errorf("storage: the token was sent to it")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write(body)
		}))
		ts.Defer(files.Close)
		srv.RewriteOrigin(apitest.StorageOrigin, files.URL)
	}
	ts.Setenv("HKR_URL", srv.URL)
	ts.Setenv("URL", srv.URL)
	ts.Defer(st.cleanup)
}

// scriptT adapts a script to what apitest needs. The server's complaints
// arrive from other goroutines, so they are kept and reported at the end.
type scriptT struct {
	ts       *testscript.TestScript
	mu       sync.Mutex
	errs     []string
	cleanups []func()
}

func (s *scriptT) Helper() {}

func (s *scriptT) Errorf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, fmt.Sprintf(format, args...))
}

func (s *scriptT) Fatalf(format string, args ...any) { s.ts.Fatalf(format, args...) }

func (s *scriptT) Cleanup(f func()) { s.cleanups = append(s.cleanups, f) }

func (s *scriptT) cleanup() {
	for _, f := range s.cleanups {
		f()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.errs {
		s.ts.Logf("fake hkr: %s", e)
	}
}

// noKeyring is a machine without a keychain.
type noKeyring struct{}

var errNoKeychain = errors.New("no keychain in tests")

func (noKeyring) Get(string, string) (string, error) { return "", errNoKeychain }
func (noKeyring) Set(string, string, string) error   { return errNoKeychain }
func (noKeyring) Delete(string, string) error        { return errNoKeychain }

// logBrowser "opens" a URL by appending it to a file.
func logBrowser(path string) func(string) error {
	return func(url string) error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f, url)
		return errors.Join(err, f.Close())
	}
}
