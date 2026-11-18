package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Exit codes (SPEC 4.6). They are a public contract: scripts and agents
// branch on them.
const (
	ExitOK    = 0 // Done.
	ExitError = 1 // HKr refused or failed: not found, not allowed, validation, network, server error.
	ExitUsage = 2 // Wrong usage (unknown flag, missing argument).
	ExitAuth  = 3 // Not signed in, a rejected or revoked token, or a read-only token asked to change something.
	ExitScan  = 4 // scans follow: the scan failed or was cancelled, or --fail-on matched.
)

// usageError marks an error caused by how the command was invoked.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// silentError has been reported already; it only carries the exit code.
type silentError struct{ code int }

func (e silentError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// classify picks the exit code and the JSON error code for err.
func classify(err error) (int, string) {
	var (
		ue usageError
		se silentError
	)
	switch {
	case err == nil:
		return ExitOK, ""
	case errors.As(err, &se):
		return se.code, ""
	case errors.As(err, &ue):
		return ExitUsage, "usage"
	case errors.Is(err, context.Canceled):
		return ExitError, "interrupted"
	}
	return ExitError, "error"
}

func exitCode(err error) int {
	code, _ := classify(err)
	return code
}

// reportError writes err to stderr: "error:" lines, or one JSON object
// under --json.
func (a *app) reportError(err error) {
	code, kind := classify(err)
	if kind == "" || errors.Is(err, context.Canceled) {
		return
	}
	w := a.env.Stderr
	if a.flags.json {
		b, _ := json.Marshal(map[string]any{"error": map[string]any{"code": kind, "message": err.Error()}})
		fmt.Fprintf(w, "%s\n", b)
		return
	}
	fmt.Fprintf(w, "error: %s\n", err)
	if code == ExitUsage {
		fmt.Fprintln(w, "Run hkr --help for usage.")
	}
}
