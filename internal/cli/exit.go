package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/auth"
	"github.com/chrom-ar/hkr-cli/internal/channel"
	"github.com/chrom-ar/hkr-cli/internal/follow"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/chrom-ar/hkr-cli/internal/target"
)

// Exit codes (SPEC 4.6). They are a public contract: scripts and agents
// branch on them.
const (
	ExitOK    = 0 // Done.
	ExitError = 1 // HKr refused or failed: not found, not allowed, validation, network, server error.
	ExitUsage = 2 // Wrong usage (unknown flag, missing argument).
	ExitAuth  = 3 // Not signed in, a rejected or revoked token, a token without the access asked for, or HKr's terms not accepted.
	ExitScan  = 4 // scans follow: the scan failed or was cancelled, or --fail-on matched.
)

// usageError marks an error caused by how the command was invoked.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// authError marks an error that means the token can't be used: exit 3.
type authError struct{ err error }

func (e authError) Error() string { return e.err.Error() }
func (e authError) Unwrap() error { return e.err }

// silentError has been reported already; it only carries the exit code.
type silentError struct{ code int }

func (e silentError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// classify picks the exit code and the JSON error code for err.
func classify(err error) (int, string) {
	var (
		ue     usageError
		ae     authError
		se     silentError
		ns     notSignedIn
		apiErr *api.Error
	)
	switch {
	case err == nil:
		return ExitOK, ""
	case errors.As(err, &se):
		return se.code, ""
	case errors.As(err, &ue), errors.Is(err, target.ErrAmbiguous), errors.Is(err, target.ErrBadRef):
		return ExitUsage, "usage"
	case termsNotAccepted(err):
		return ExitAuth, "terms_not_accepted"
	case errors.As(err, &ns), errors.Is(err, channel.ErrRevoked):
		return ExitAuth, "unauthorized"
	case errors.As(err, &ae):
		return ExitAuth, "unauthorized"
	case errors.Is(err, auth.ErrDenied):
		return ExitAuth, "access_denied"
	case errors.Is(err, auth.ErrExpired):
		return ExitAuth, "expired_token"
	case errors.As(err, &apiErr):
		switch {
		case apiErr.Status == http.StatusUnauthorized,
			apiErr.Code == "read_only", apiErr.Code == "setup_scope", apiErr.Code == "access_denied", apiErr.Code == "expired_token":
			return ExitAuth, apiErr.Code
		case apiErr.Status == http.StatusBadRequest:
			return ExitUsage, apiErr.Code
		}
		return ExitError, apiErr.Code
	case errors.Is(err, context.Canceled):
		return ExitError, "interrupted"
	}
	return ExitError, "error"
}

func exitCode(err error) int {
	code, _ := classify(err)
	return code
}

// termsNotAccepted is HKr refusing until the person accepts its terms,
// which they do in the web app: hkr never accepts for them.
func termsNotAccepted(err error) bool {
	var (
		apiErr *api.Error
		je     *channel.JoinError
		lost   *follow.AccessLostError
	)
	const code = "terms_not_accepted"
	return errors.As(err, &apiErr) && apiErr.Code == code ||
		errors.As(err, &je) && je.Code == code ||
		errors.As(err, &lost) && lost.Code == code
}

// hint is what to do next, when there's something to say.
func (a *app) hint(err error) string {
	var (
		ns     notSignedIn
		apiErr *api.Error
	)
	switch {
	case termsNotAccepted(err):
		return "only you can accept them, in a browser; then run this again"
	case errors.As(err, &ns):
		return "run: hkr login (or set HKR_TOKEN)"
	case errors.Is(err, api.ErrUnauthorized), errors.As(err, new(authError)):
		return "run: hkr login"
	case errors.As(err, &apiErr) && apiErr.Code == "setup_scope":
		return "this token can't change setup: run hkr login --setup"
	case errors.Is(err, api.ErrReadOnly):
		return "this token is read-only; create one with write access, or run: hkr login"
	case errors.Is(err, target.ErrAmbiguous):
		return "or set a default for this HKr: hkr use <org>"
	case errors.As(err, &apiErr) && (apiErr.Code == "insufficient_balance" || apiErr.Code == "no_plan"):
		if a.session != nil {
			return "add credit: " + a.session.url + "/organization/billing"
		}
		return "add credit in HKr, under Organization › Billing"
	case errors.As(err, &apiErr) && apiErr.Code == "not_verified" && a.domainArg != "":
		return "run: hkr domains verify " + a.domainArg
	case errors.As(err, &apiErr) && apiErr.Code == "not_attested" && a.domainArg != "":
		return "run: hkr domains attest " + a.domainArg + " --name '<your full name>'"
	case errors.As(err, &apiErr) && apiErr.Code == "run_already_active" && a.domainArg != "":
		return "see it: hkr scans show " + a.domainArg
	case errors.As(err, &apiErr) && apiErr.Code == "forbidden":
		if a.org != "" {
			return "ask an admin of " + a.org
		}
		return "ask an admin of the organization"
	}
	return ""
}

// reportError writes err to stderr: "error:" lines, or one JSON object
// under --json.
func (a *app) reportError(err error) {
	code, kind := classify(err)
	if kind == "" || errors.Is(err, context.Canceled) {
		return
	}
	var apiErr *api.Error
	fields := map[string][]string{}
	if errors.As(err, &apiErr) {
		fields = apiErr.Fields
	}
	msg := render.Safe(err.Error())
	w := a.env.Stderr
	if a.flags.json {
		e := map[string]any{"code": kind, "message": msg}
		if len(fields) > 0 {
			e["fields"] = fields
		}
		if apiErr != nil && apiErr.URL != "" {
			e["url"] = render.Safe(apiErr.URL)
		}
		b, _ := json.Marshal(map[string]any{"error": e})
		fmt.Fprintf(w, "%s\n", b)
		return
	}
	fmt.Fprintf(w, "error: %s\n", msg)
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		fmt.Fprintf(w, "  %s: %s\n", render.Safe(name), render.Safe(strings.Join(fields[name], ", ")))
	}
	if h := a.hint(err); h != "" {
		fmt.Fprintln(w, h)
	}
	var ue usageError
	if code == ExitUsage && (errors.As(err, &ue) || errors.Is(err, target.ErrAmbiguous) || errors.Is(err, target.ErrBadRef)) {
		fmt.Fprintln(w, "Run hkr --help for usage.")
	}
}
