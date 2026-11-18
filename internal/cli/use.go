package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/auth"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newUseCmd(a *app) *cobra.Command {
	var clear bool
	c := &cobra.Command{
		Use:   "use [<org>]",
		Short: "Set the organization commands use by default",
		Long: "Set the organization that commands use on this HKr when --org and\n" +
			"HKR_ORG don't say. Without an argument, show it.",
		Example: "  hkr use acme\n  hkr use\n  hkr use --clear",
		Args:    argsBetween(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.resolve()
			if err != nil {
				return err
			}
			cfg, _, err := a.config()
			if err != nil {
				return err
			}
			if clear {
				if len(args) > 0 {
					return usageError{errors.New("--clear takes no argument")}
				}
				delete(cfg.Defaults, s.url)
				if err := cfg.Save(a.configPath()); err != nil {
					return err
				}
				a.out("No default on %s.", s.url)
				return nil
			}
			if len(args) == 0 {
				d, ok := cfg.Defaults[s.url]
				if !ok {
					a.out("No default on %s.", s.url)
					a.progress("Set one with: hkr use <org>")
					return nil
				}
				a.out("%s", render.Safe(defaultText(d)))
				return nil
			}
			org := strings.TrimSpace(args[0])
			if org == "" || strings.Contains(org, "/") {
				return usageError{fmt.Errorf("%q: expected an organization's slug", args[0])}
			}
			// Check it exists, and that this token can see it.
			c, _, err := a.client()
			if err != nil {
				return err
			}
			if _, _, err := c.Org(cmd.Context(), org); err != nil {
				return err
			}
			if cfg.Defaults == nil {
				cfg.Defaults = map[string]auth.Default{}
			}
			d := auth.Default{Org: org}
			cfg.Defaults[s.url] = d
			if err := cfg.Save(a.configPath()); err != nil {
				return err
			}
			a.out("Commands on %s now use %s.", s.url, render.Safe(defaultText(d)))
			a.progress("--org and HKR_ORG still win.")
			return nil
		},
	}
	c.Flags().BoolVar(&clear, "clear", false, "forget the default")
	return c
}

func defaultText(d auth.Default) string { return d.Org }
