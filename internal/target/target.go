// Package target chooses the organization a command works on: --org,
// HKR_ORG, the default from hkr use, or the only one there is. When it's
// ambiguous, the error lists the choices. It also reads the ids and web
// URLs people paste.
package target

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
)

// Errors that are usage mistakes: the person must say which, or gave
// something that isn't what the command takes.
var (
	ErrAmbiguous = errors.New("say which")
	ErrBadRef    = errors.New("not a reference")
)

// API is what choosing needs from HKr.
type API interface {
	Me(ctx context.Context) (*api.Me, []byte, error)
}

// Resolver chooses, asking HKr only when nothing named the organization,
// and only once.
type Resolver struct {
	API     API
	Flag    string // --org
	Env     string // HKR_ORG
	Default string // What hkr use stored for this HKr.

	org string
}

// Org is the organization's slug.
func (r *Resolver) Org(ctx context.Context) (string, error) {
	if g := first(r.Flag, r.Env, r.Default); g != "" {
		return g, nil
	}
	if r.org != "" {
		return r.org, nil
	}
	me, _, err := r.API.Me(ctx)
	if err != nil {
		return "", err
	}
	slugs := make([]string, 0, len(me.Organizations))
	for _, o := range me.Organizations {
		slugs = append(slugs, o.Slug)
	}
	org, err := pick(slugs)
	r.org = org
	return org, err
}

// Explicit reports whether the organization was named, so nothing need be
// asked.
func (r *Resolver) Explicit() bool { return first(r.Flag, r.Env, r.Default) != "" }

func first(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

type choiceError struct{ msg string }

func (e *choiceError) Error() string        { return e.msg }
func (e *choiceError) Is(target error) bool { return target == ErrAmbiguous }

func pick(slugs []string) (string, error) {
	switch len(slugs) {
	case 0:
		return "", &choiceError{"you're in no organizations"}
	case 1:
		return slugs[0], nil
	}
	return "", &choiceError{fmt.Sprintf("you're in %d organizations; pass --org %s (or %s)", len(slugs), slugs[0], strings.Join(slugs[1:], ", "))}
}

// id is an Hkr.Id: 12 characters from an alphabet without 0, 1, I, O
// and l.
const id = `[2-9A-HJ-NP-Za-km-z]{12}`

var (
	idRe          = regexp.MustCompile(`^` + id + `$`)
	findingPathRe = regexp.MustCompile(`^(?:/domains/` + id + `)?/findings/(` + id + `)/?$`)
)

type refError struct{ msg string }

func (e *refError) Error() string        { return e.msg }
func (e *refError) Is(target error) bool { return target == ErrBadRef }

// ParseFinding reads a finding's id, or its web URL (/findings/:id or
// /domains/:domain/findings/:id) on the HKr at origin.
func ParseFinding(ref, origin string) (string, error) {
	if idRe.MatchString(ref) {
		return ref, nil
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", &refError{fmt.Sprintf("%q isn't a finding id or a finding URL", ref)}
	}
	if o := strings.ToLower(u.Scheme + "://" + u.Host); o != origin {
		return "", &refError{fmt.Sprintf("that finding is on %s, but the HKr in use is %s; pass --url %s", o, origin, o)}
	}
	m := findingPathRe.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return "", &refError{fmt.Sprintf("%q isn't a finding URL (expected …/findings/<id>)", ref)}
	}
	return m[1], nil
}

// IsID reports whether s is shaped like an HKr id.
func IsID(s string) bool { return idRe.MatchString(s) }
