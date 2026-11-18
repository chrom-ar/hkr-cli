package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
)

// stepTitles name the checklist's steps for people. HKr decides which
// steps there are and in what order; this only says them.
var stepTitles = map[string]string{
	"domain":    "Add a domain",
	"verify":    "Prove you control it",
	"authorize": "State you're authorized",
	"launch":    "First scan",
	"git_host":  "Connect a git host",
	"link":      "Link repositories to hosts",
	"schedule":  "Schedule scans",
}

// stepCommand is a step's title and the hkr command that does it: ""
// when the CLI has none (a step HKr added later) or the caller's role
// can't do it.
func stepCommand(s api.SetupStep) (title, command string) {
	title, known := stepTitles[s.Key]
	if !known {
		return render.Safe(s.Key), ""
	}
	if !s.CanAct {
		return title, ""
	}
	root := "<root-domain>"
	if s.Domain != nil {
		root = render.Safe(s.Domain.RootDomain)
	}
	switch s.Key {
	case "domain":
		return title, "hkr domains create <root-domain>"
	case "verify":
		return title, "hkr domains verify " + root
	case "authorize":
		return title, "hkr domains attest " + root + " --name '<your full name>'"
	case "launch":
		return title, "hkr scans start " + root
	case "git_host":
		return title, "hkr repos connect github"
	case "link":
		return title, "hkr links list --status proposed"
	case "schedule":
		return title, "hkr schedules create " + root + " --every week --weekday mon --at 03:00"
	}
	return title, ""
}

// nextText is the "Next: …" line for a step.
func nextText(s *api.SetupStep, org string) string {
	label := "Next"
	if s.Optional {
		label = "Next (optional)"
	}
	title, cmd := stepCommand(*s)
	switch {
	case cmd != "":
		return label + ": " + cmd
	case !s.CanAct && title != "":
		return fmt.Sprintf("%s: ask an admin of %s to %s", label, render.Safe(org), strings.ToLower(title[:1])+title[1:])
	}
	return label + ": " + render.Safe(s.URL)
}

// nextLine is the checklist's Next line: HKr's next step or, when every
// step left is one the caller's role can't do, who to ask; "" when
// nothing is left.
func nextLine(s *api.Setup, org string) string {
	if s.Next != nil {
		return nextText(s.Next, org)
	}
	for i := range s.Steps {
		if st := &s.Steps[i]; !st.Done && !st.CanAct {
			return nextText(st, org)
		}
	}
	return ""
}

// stepDetail is what a step's line says after its title.
func stepDetail(s api.SetupStep) string {
	var d struct {
		Domains     int `json:"domains"`
		Challenge   *api.Challenge
		ActiveScan  *int   `json:"active_scan"`
		BalanceUSD  string `json:"balance_usd"`
		Funded      *bool  `json:"funded"`
		BillingURL  string `json:"billing_url"`
		Connections []struct {
			Provider string `json:"provider"`
			Name     string `json:"name"`
		} `json:"connections"`
		State     string `json:"state"`
		Proposed  int    `json:"proposed"`
		Confirmed int    `json:"confirmed"`
		Schedules int    `json:"schedules"`
	}
	_ = json.Unmarshal(s.Detail, &d)
	root := ""
	if s.Domain != nil {
		root = render.Safe(s.Domain.RootDomain)
	}
	withRoot := func(text string) string {
		if root == "" {
			return text
		}
		return root + ": " + text
	}
	plural := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch s.Key {
	case "domain":
		if d.Domains == 0 {
			return "none yet"
		}
		return plural(d.Domains, "domain")
	case "verify":
		if !s.Done && d.Challenge != nil && d.Challenge.Record != nil {
			return withRoot("publish TXT " + render.Safe(d.Challenge.Record.Name))
		}
		if !s.Done && s.Minutes > 0 {
			return fmt.Sprintf("%s (~%g min)", root, s.Minutes)
		}
		return root
	case "launch":
		switch {
		case s.Done:
			return root
		case d.ActiveScan != nil:
			return withRoot(fmt.Sprintf("scan %d running", *d.ActiveScan))
		case d.Funded != nil && !*d.Funded:
			return withRoot("add credit: " + render.Safe(d.BillingURL))
		case len(s.BlockedBy) > 0:
			return withRoot(fmt.Sprintf("waits for %s ($%s of credit)", render.Safe(strings.Join(s.BlockedBy, ", ")), render.Safe(d.BalanceUSD)))
		}
		return withRoot(fmt.Sprintf("ready ($%s of credit)", render.Safe(d.BalanceUSD)))
	case "git_host":
		if len(d.Connections) == 0 {
			return "none"
		}
		var names []string
		for _, c := range d.Connections {
			names = append(names, fmt.Sprintf("%s (%s)", c.Provider, render.Safe(c.Name)))
		}
		return strings.Join(names, ", ")
	case "link":
		switch d.State {
		case "to_decide":
			return withRoot(plural(d.Proposed, "link") + " to decide")
		case "syncing":
			return withRoot("HKr is still reading your repositories")
		case "none_proposed":
			return withRoot("none proposed; link one: hkr links add")
		}
		return withRoot(fmt.Sprintf("%d linked", d.Confirmed))
	case "schedule":
		if d.Schedules == 0 {
			return withRoot("none")
		}
		return withRoot(plural(d.Schedules, "schedule"))
	case "authorize":
		return root
	}
	if root != "" {
		return root + "  " + render.Safe(s.URL)
	}
	return render.Safe(s.URL)
}
