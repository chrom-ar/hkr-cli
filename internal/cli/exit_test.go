package cli

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/channel"
	"github.com/chrom-ar/hkr-cli/internal/follow"
)

// A token without the setup toggle is refused like a read-only one: exit
// 3, and how to get one that can.
func TestSetupScopeIsAnAuthError(t *testing.T) {
	err := &api.Error{Status: http.StatusForbidden, Code: "setup_scope", Message: "This token can't change setup. Make one with setup changes on."}
	if got := exitCode(err); got != ExitAuth {
		t.Errorf("exit = %d, want %d", got, ExitAuth)
	}
	var stderr bytes.Buffer
	a := &app{env: withDefaults(Env{Stderr: &stderr})}
	a.reportError(err)
	if !strings.Contains(stderr.String(), "this token can't change setup: run hkr login --setup\n") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// Unaccepted terms, from the API or the live stream, exit 3 and say only
// the person can accept them.
func TestTermsNotAcceptedIsAnAuthError(t *testing.T) {
	for name, err := range map[string]error{
		"api":    &api.Error{Status: http.StatusForbidden, Code: "terms_not_accepted", Message: "Accept HKr's terms first: http://hkr.test/terms/accept", URL: "http://hkr.test/terms/accept"},
		"join":   &channel.JoinError{Code: "terms_not_accepted", Message: "Accept HKr's terms first: http://hkr.test/terms/accept"},
		"stream": &follow.AccessLostError{Code: "terms_not_accepted"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, kind := classify(err); code != ExitAuth || kind != "terms_not_accepted" {
				t.Errorf("classify = %d, %q", code, kind)
			}
			var stderr bytes.Buffer
			a := &app{env: withDefaults(Env{Stderr: &stderr})}
			a.reportError(err)
			if !strings.Contains(stderr.String(), "only you can accept them, in a browser; then run this again\n") {
				t.Errorf("stderr = %q", stderr.String())
			}
		})
	}
}
