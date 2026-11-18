package cli

import (
	_ "embed"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed guide.md
var guideText string

func newGuideCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "How to set HKr up, step by step (for people and agents)",
		Long: "Print the guide to setting HKr up: the steps, the loop an agent runs,\n" +
			"and what only a person can do. It matches this version's commands.",
		Example: "  hkr guide\n  hkr guide > HKR.md",
		Args:    noArgs,
		RunE: func(*cobra.Command, []string) error {
			a.out("%s", strings.TrimRight(guideText, "\n"))
			return nil
		},
	}
}
