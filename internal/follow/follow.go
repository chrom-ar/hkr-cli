// Package follow is the logic of `hkr scans follow` (SPEC 4.4): the
// scan's timeline over REST, then live steps, status changes and findings
// over the scan's channel, reconnecting and filling the gap when the
// socket drops, until the scan ends. It prints nothing itself: what
// happens goes to the caller's functions.
package follow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/channel"
)

const (
	// backoffMax caps the wait between reconnects.
	backoffMax = 30 * time.Second
	// maxFirstAttempts is how many times to try a socket that never opened.
	maxFirstAttempts = 5
)

// ErrNeverOpened matches the error Run returns when the socket never
// opened after 5 attempts.
var ErrNeverOpened = errors.New("this HKr may not support following yet")

// NeverOpenedError is the error Run returns when the socket never opened.
type NeverOpenedError struct {
	URL string
	Err error
}

func (e *NeverOpenedError) Error() string {
	return "couldn't open the live stream at " + e.URL + "; this HKr may not support following yet"
}

// Is makes a NeverOpenedError match ErrNeverOpened.
func (e *NeverOpenedError) Is(target error) bool { return target == ErrNeverOpened }

// Unwrap returns the last attempt's error.
func (e *NeverOpenedError) Unwrap() error { return e.Err }

// REST reads the scan over HKr's REST API.
type REST interface {
	// Scan is the scan followed.
	Scan(ctx context.Context) (*api.Scan, error)
	// Steps reads the timeline from cursor ("" for the start) and returns
	// the cursor for newer steps ("" once the scan is over).
	Steps(ctx context.Context, cursor string) ([]api.Step, string, error)
}

// Options configure Run. REST, Dial, Topic and Step are required.
type Options struct {
	REST REST
	// Dial opens the socket, passing onPush as channel.Options.OnPush.
	Dial func(ctx context.Context, onPush func(channel.Push)) (*channel.Conn, error)
	// URL names the socket in the error when it never opens.
	URL string
	// Topic is the scan's channel: scan:<org>/<scan id>.
	Topic func(*api.Scan) string
	// Step is told each step, oldest first, never one twice. An error ends
	// Run with it.
	Step func(api.Step) error
	// Scan, when set, is told the scan whenever it changes.
	Scan func(api.Scan)
	// Finding, when set, is told each finding the scan records or
	// re-sights, or that someone changes.
	Finding func(api.Finding)
	// Reconnecting, when set, is told the socket dropped or couldn't
	// open, why, and how long until the next try.
	Reconnecting func(in time.Duration, cause error)
	// Sleep waits between tries; nil is api.Sleep.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Run follows the scan until it ends, and returns it as it ended.
// Otherwise it returns ctx's error when ctx ends, or:
//   - an error matching channel.ErrRevoked when the token loses access
//     (access_lost unauthorized), or api.ErrNotFound when the scan goes
//     out of its reach (access_lost not_found);
//   - the *channel.JoinError of a refused join;
//   - the *api.Error of a REST answer not worth retrying: after a failed
//     connect, REST is asked whether the token still works;
//   - a *NeverOpenedError after 5 failed attempts, if the socket never
//     opened; once it has, it is retried forever;
//   - Step's error.
//
// Network errors, 5xx and 429 from REST, and dropped sockets are retried
// with a backoff of 1, 2, 4 … 30 s.
func Run(ctx context.Context, o Options) (*api.Scan, error) {
	if o.REST == nil || o.Dial == nil || o.Topic == nil || o.Step == nil {
		return nil, errors.New("follow: REST, Dial, Topic and Step are required")
	}
	if o.Sleep == nil {
		o.Sleep = api.Sleep
	}
	f := &follower{o: o, seen: map[string]bool{}}
	return f.run(ctx)
}

type follower struct {
	o      Options
	seen   map[string]bool
	cursor string
}

// AccessLostError is the server ending the stream for lost access.
type AccessLostError struct{ Code string }

func (e *AccessLostError) Error() string {
	switch e.Code {
	case "unauthorized":
		return "HKr closed the stream: the token was revoked"
	case "terms_not_accepted":
		return "HKr closed the stream: accept HKr's terms in the web app first"
	}
	return "HKr closed the stream: the scan is out of this token's reach"
}

// Is makes it match channel.ErrRevoked or api.ErrNotFound; unaccepted
// terms match neither.
func (e *AccessLostError) Is(target error) bool {
	switch e.Code {
	case "unauthorized":
		return target == channel.ErrRevoked
	case "terms_not_accepted":
		return false
	}
	return target == api.ErrNotFound
}

func (f *follower) run(ctx context.Context) (*api.Scan, error) {
	var s *api.Scan
	scan := func() error {
		var err error
		s, err = f.o.REST.Scan(ctx)
		return err
	}
	if err := f.retryREST(ctx, scan); err != nil {
		return nil, err
	}
	if err := f.fill(ctx); err != nil {
		return nil, err
	}
	if !s.Over() && f.cursor == "" {
		// The timeline ended though the scan looked running: ask again.
		if err := f.retryREST(ctx, scan); err != nil {
			return nil, err
		}
	}
	if s.Over() {
		return s, nil
	}
	topic := f.o.Topic(s)

	backoff, attempts, everOpened := time.Second, 0, false
	for {
		res, opened, retry, err := f.session(ctx, topic)
		if !retry {
			return res, err
		}
		if opened {
			everOpened, attempts, backoff = true, 0, time.Second
		} else if !everOpened {
			attempts++
			if attempts >= maxFirstAttempts {
				return nil, &NeverOpenedError{URL: f.o.URL, Err: err}
			}
		}
		if f.o.Reconnecting != nil {
			f.o.Reconnecting(backoff, err)
		}
		if err := f.o.Sleep(ctx, backoff); err != nil {
			return nil, err
		}
		backoff = min(backoff*2, backoffMax)
		if !opened {
			// A refused or failed connect may be a revoked token: REST says.
			if _, err := f.o.REST.Scan(ctx); err != nil && !passing(err) {
				return nil, err
			}
		}
	}
}

// retryREST runs call until it succeeds or fails for good.
func (f *follower) retryREST(ctx context.Context, call func() error) error {
	backoff := time.Second
	for {
		err := call()
		if err == nil || !passing(err) {
			return err
		}
		if f.o.Reconnecting != nil {
			f.o.Reconnecting(backoff, err)
		}
		if err := f.o.Sleep(ctx, backoff); err != nil {
			return err
		}
		backoff = min(backoff*2, backoffMax)
	}
}

// fill reads the timeline from the cursor and reports what's new,
// retrying passing failures.
func (f *follower) fill(ctx context.Context) error {
	err := f.retryREST(ctx, func() error {
		steps, next, err := f.o.REST.Steps(ctx, f.cursor)
		if err != nil {
			return err
		}
		f.cursor = next
		for _, s := range steps {
			if err := f.step(s); err != nil {
				return &stepError{err}
			}
		}
		return nil
	})
	var se *stepError
	if errors.As(err, &se) {
		return se.err
	}
	return err
}

type stepError struct{ err error }

func (e *stepError) Error() string { return e.err.Error() }

func (f *follower) step(s api.Step) error {
	if s.ID == "" || f.seen[s.ID] {
		return nil
	}
	f.seen[s.ID] = true
	return f.o.Step(s)
}

// passing reports whether a REST error may pass: the network, 5xx, 429.
func passing(err error) bool {
	var e *api.Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code == "network" || e.Status >= 500 || e.Status == http.StatusTooManyRequests
}

// queue holds pushes from the read goroutine until the main one takes
// them.
type queue struct {
	mu     sync.Mutex
	pushes []channel.Push
	signal chan struct{}
}

func (q *queue) add(p channel.Push) {
	q.mu.Lock()
	q.pushes = append(q.pushes, p)
	q.mu.Unlock()
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *queue) take() []channel.Push {
	q.mu.Lock()
	defer q.mu.Unlock()
	p := q.pushes
	q.pushes = nil
	return p
}

// session is one socket: connect, join, close the gap, stream. It ends
// the follow (retry false) with the scan as it ended or an error, or asks
// for another try; opened says whether it joined.
func (f *follower) session(ctx context.Context, topic string) (end *api.Scan, opened, retry bool, err error) {
	q := &queue{signal: make(chan struct{}, 1)}
	conn, err := f.o.Dial(ctx, q.add)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, false, ctx.Err()
		}
		return nil, false, true, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.Join(ctx, topic, nil); err != nil {
		var je *channel.JoinError
		switch {
		case ctx.Err() != nil:
			return nil, false, false, ctx.Err()
		case errors.Is(err, channel.ErrRevoked), errors.As(err, &je) && !errors.Is(err, channel.ErrDropped):
			return nil, false, false, err
		}
		return nil, false, true, err
	}
	if err := f.fill(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, true, false, ctx.Err()
		}
		return nil, true, false, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, true, false, ctx.Err()
		case <-q.signal:
			if s, done, err := f.handle(q.take()); done {
				return s, true, false, err
			}
		case <-conn.Done():
			if s, done, err := f.handle(q.take()); done {
				return s, true, false, err
			}
			err := conn.Err()
			switch {
			case ctx.Err() != nil:
				return nil, true, false, ctx.Err()
			case errors.Is(err, channel.ErrRevoked):
				return nil, true, false, err
			}
			return nil, true, true, err
		}
	}
}

// handle reports pushes; done means the follow is over.
func (f *follower) handle(pushes []channel.Push) (*api.Scan, bool, error) {
	for _, p := range pushes {
		switch p.Event {
		case "step":
			var s api.Step
			if json.Unmarshal(p.Payload, &s) == nil {
				if err := f.step(s); err != nil {
					return nil, true, err
				}
			}
		case "scan":
			var s api.Scan
			if json.Unmarshal(p.Payload, &s) == nil && f.o.Scan != nil {
				f.o.Scan(s)
			}
		case "finding":
			var fd api.Finding
			if json.Unmarshal(p.Payload, &fd) == nil && f.o.Finding != nil {
				f.o.Finding(fd)
			}
		case "ended":
			var e struct {
				Scan api.Scan `json:"scan"`
			}
			if err := json.Unmarshal(p.Payload, &e); err != nil {
				return nil, true, fmt.Errorf("follow: the end of the scan isn't the JSON expected: %w", err)
			}
			return &e.Scan, true, nil
		case "access_lost":
			var e struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(p.Payload, &e)
			return nil, true, &AccessLostError{Code: e.Error.Code}
		}
	}
	return nil, false, nil
}
