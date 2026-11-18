package cli

import (
	"strconv"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newOrgsCmd(a *app) *cobra.Command {
	return group(a, "orgs", "Your organizations", "  hkr orgs list", newOrgsListCmd(a))
}

func newOrgsListCmd(a *app) *cobra.Command {
	var l listFlags
	c := &cobra.Command{
		Use:     "list",
		Short:   "Your organizations, your role, open critical and high findings",
		Example: "  hkr orgs list\n  hkr orgs list --json",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			return printList(cmd.Context(), a, cl, &l, "/orgs", nil, func(os []api.Organization) string {
				rows := make([][]string, 0, len(os))
				for _, o := range os {
					rows = append(rows, []string{o.Slug, o.Name, o.Role, strconv.Itoa(o.OpenFindings), o.Locale})
				}
				return render.Table([]string{"SLUG", "NAME", "ROLE", "OPEN CRITICAL/HIGH", "LANGUAGE"}, rows)
			}, "You're in no organizations.")
		},
	}
	l.add(c)
	return c
}
