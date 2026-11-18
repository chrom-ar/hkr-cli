package cli

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/chrom-ar/hkr-cli/internal/api"
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
