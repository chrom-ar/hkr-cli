// Package cli holds hkr's commands: their flags, arguments and output.
// It stays thin: the work is done by the packages it wires together.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// Build describes the binary, as stamped in at build time.
type Build struct {
	Version string
	Commit  string
	Date    string
}

// Env is everything a command may touch outside itself. main fills it
// with the real thing; tests with fakes.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	Getenv func(string) string
	Build  Build
}

// Run executes the command line args (without the program name) and
// returns the process exit code.
func Run(ctx context.Context, args []string, env Env) int {
	a := &app{env: withDefaults(env)}
	root := newRootCmd(a)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil {
		a.reportError(err)
	}
	return exitCode(err)
}

func withDefaults(env Env) Env {
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	if env.Stdin == nil {
		env.Stdin = strings.NewReader("")
	}
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	return env
}

// app is one run of hkr: its environment, global flags, and what it
// worked out along the way.
type app struct {
	env   Env
	flags globalFlags
}

type globalFlags struct {
	url, org, lang                     string
	json, yes, quiet, verbose, noColor bool
}

// setup applies the global flags, once they are parsed.
func (a *app) setup() error {
	if a.env.Getenv("HKR_DEBUG") == "1" {
		a.flags.verbose = true
	}
	if a.env.Getenv("NO_COLOR") != "" {
		a.flags.noColor = true
	}
	return nil
}

const rootLong = `hkr uses HKr from the terminal: what it found in your domains and
code, the evidence and fixes, and the scans that test them. For people,
their coding agents and CI.

Output is for people by default; --json prints HKr's JSON as is, and
--markdown HKr's text for an AI, where offered.

Which organization: --org (or HKR_ORG), implied when you have one.
Credentials: HKR_TOKEN, else the token hkr login stored.

Exit codes: 0 done, 1 HKr refused or failed, 2 wrong usage, 3 not
signed in, a revoked token, or a read-only token asked to change
something; scans follow adds 4, the scan failed or --fail-on matched.`

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "hkr <command>",
		Short: "Use HKr from the terminal",
		Long:  rootLong,
		Example: "  hkr login\n" +
			"  hkr findings list --severity high\n" +
			"  hkr findings show Fx7Kq2XpAb2m --markdown > finding.md\n" +
			"  hkr scans start shop.example.com --preset first_look --follow",
		Version: fmt.Sprintf("%s (commit %s, built %s)",
			a.env.Build.Version, a.env.Build.Commit, a.env.Build.Date),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown command %q", args[0])}
			}
			return nil
		},
		// No command: the usage, on stderr, as a usage mistake.
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetOut(a.env.Stderr)
			if err := cmd.Usage(); err != nil {
				return err
			}
			return silentError{ExitUsage}
		},
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return a.setup()
		},
	}
	root.SetVersionTemplate("hkr {{.Version}}\n")
	root.SetOut(a.env.Stdout)
	root.SetErr(a.env.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err}
	})

	f := root.PersistentFlags()
	f.StringVar(&a.flags.url, "url", "", "the HKr to use (or $HKR_URL; default: the last login)")
	f.StringVar(&a.flags.org, "org", "", "the organization (or $HKR_ORG; implied when you have one)")
	f.BoolVar(&a.flags.json, "json", false, "print HKr's JSON as is")
	f.StringVar(&a.flags.lang, "lang", "", "language of HKr's messages: en or es (or $HKR_LANG)")
	f.BoolVarP(&a.flags.yes, "yes", "y", false, "don't ask before spending or stopping")
	f.BoolVarP(&a.flags.quiet, "quiet", "q", false, "only errors and the essential result")
	f.BoolVarP(&a.flags.verbose, "verbose", "v", false, "requests and timings on stderr (or $HKR_DEBUG=1)")
	f.BoolVar(&a.flags.noColor, "no-color", false, "no colours (or $NO_COLOR)")

	return root
}
