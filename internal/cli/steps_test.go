package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/api/apitest"
)

// Every step HKr's fixtures name has a title and, when actionable, a
// command.
func TestStepCommandCoversEveryFixtureKey(t *testing.T) {
	for _, name := range apitest.Names() {
		if !strings.HasPrefix(name, "setup.get.") {
			continue
		}
		f := apitest.Load(t, name)
		var s api.Setup
		if err := json.Unmarshal(f.Response.Body, &s); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, st := range s.Steps {
			if _, known := stepTitles[st.Key]; !known {
				t.Errorf("%s: no title for step %q", name, st.Key)
			}
			if title, cmd := stepCommand(st); title == "" || (st.CanAct && cmd == "") {
				t.Errorf("%s: step %q gives title %q, command %q", name, st.Key, title, cmd)
			}
		}
	}
}

func TestStepCommands(t *testing.T) {
	shop := &api.DomainRef{ID: "DomShop7Kq2X", RootDomain: "shop.example.com"}
	for key, want := range map[string]string{
		"domain":    "hkr domains create <root-domain>",
		"verify":    "hkr domains verify shop.example.com",
		"authorize": "hkr domains attest shop.example.com --name '<your full name>'",
		"launch":    "hkr scans start shop.example.com",
		"git_host":  "hkr repos connect github",
		"link":      "hkr links list --status proposed",
		"schedule":  "hkr schedules create shop.example.com --every week --weekday mon --at 03:00",
	} {
		st := api.SetupStep{Key: key, CanAct: true, Domain: shop}
		if key == "domain" || key == "git_host" {
			st.Domain = nil
		}
		if _, got := stepCommand(st); got != want {
			t.Errorf("%s: command %q, want %q", key, got, want)
		}
	}
}

// A step HKr adds later has no command here: its URL is shown instead.
func TestUnknownKeyHasNoCommand(t *testing.T) {
	title, cmd := stepCommand(api.SetupStep{Key: "payment", CanAct: true, URL: "http://hkr.test/x"})
	if cmd != "" || title != "payment" {
		t.Errorf("title %q, command %q", title, cmd)
	}
	if got := nextText(&api.SetupStep{Key: "payment", CanAct: true, URL: "http://hkr.test/x"}, "acme"); got != "Next: http://hkr.test/x" {
		t.Errorf("next = %q", got)
	}
}

// A step the caller's role can't do points at an admin, not at a
// command that would fail.
func TestMemberGetsNoCommand(t *testing.T) {
	st := api.SetupStep{Key: "git_host", CanAct: false, Optional: true}
	if _, cmd := stepCommand(st); cmd != "" {
		t.Errorf("command %q", cmd)
	}
	if got := nextText(&st, "acme"); got != "Next (optional): ask an admin of acme to connect a git host" {
		t.Errorf("next = %q", got)
	}
}

// splitCommand splits a command line as a shell would, for the simple
// quoting the hints and the guide use ('…').
func splitCommand(line string) []string {
	var args []string
	var cur strings.Builder
	quoted, any := false, false
	for _, r := range line {
		switch {
		case r == '\'':
			quoted, any = !quoted, true
		case r == ' ' && !quoted:
			if any {
				args = append(args, cur.String())
				cur.Reset()
				any = false
			}
		default:
			cur.WriteRune(r)
			any = true
		}
	}
	if any {
		args = append(args, cur.String())
	}
	return args
}

// runsWithoutUsageError runs a suggested command with no HKr to reach:
// it may fail to connect, never as a usage mistake.
func runsWithoutUsageError(t *testing.T, line string) {
	t.Helper()
	args := splitCommand(strings.TrimPrefix(line, "hkr "))
	var stderr strings.Builder
	code := Run(t.Context(), args, Env{
		Stderr:          &stderr,
		StdinIsTerminal: true,
		Getenv: func(k string) string {
			if k == "HKR_URL" {
				return "http://127.0.0.1:1"
			}
			return ""
		},
	})
	if code == ExitUsage {
		t.Errorf("%q is a usage mistake: %s", line, stderr.String())
	}
}

// Every command a hint suggests runs.
func TestStepCommandsRun(t *testing.T) {
	shop := &api.DomainRef{ID: "DomShop7Kq2X", RootDomain: "shop.example.com"}
	for key := range stepTitles {
		if _, cmd := stepCommand(api.SetupStep{Key: key, CanAct: true, Domain: shop}); cmd != "" {
			runsWithoutUsageError(t, cmd)
		}
	}
}

// A member whose remaining steps are admin-only is told to ask an admin,
// on the row and on the Next line.
func TestStatusTellsMembersToAskAnAdmin(t *testing.T) {
	s := &api.Setup{Complete: true, Steps: []api.SetupStep{
		{Key: "domain", Done: true, CanAct: true, Detail: json.RawMessage(`{"domains":1}`)},
		{Key: "git_host", Optional: true, CanAct: false, Detail: json.RawMessage(`{"connections":[]}`)},
	}}
	text := statusText(s, "acme")
	if !strings.Contains(text, "Connect a git host") || !strings.Contains(text, "ask an admin of acme") {
		t.Errorf("status = %q", text)
	}
	if got := nextLine(s, "acme"); got != "Next (optional): ask an admin of acme to connect a git host" {
		t.Errorf("next = %q", got)
	}
}

// Text from HKr reaches the terminal neutralized.
func TestStatusNeutralizesServerText(t *testing.T) {
	evil := "\x1b]8;;http://evil\x07x"
	s := &api.Setup{Steps: []api.SetupStep{{Key: evil, CanAct: true, URL: "http://hkr.test/" + evil, BlockedBy: []string{evil}}}}
	s.Next = &s.Steps[0]
	for _, out := range []string{statusText(s, "acme"), nextLine(s, "acme"), nextText(s.Next, evil)} {
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("control characters in %q", out)
		}
	}
}
