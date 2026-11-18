package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/spf13/cobra"
)

func newFindingsEvidenceCmd(a *app) *cobra.Command {
	var (
		all bool
		out string
	)
	c := &cobra.Command{
		Use:   "evidence <id|url> [<path>…]",
		Short: "A finding's evidence files: list them, or download them",
		Long: "List a finding's evidence files (requests, responses, screenshots),\n" +
			"newest scan first. Name paths, or --all, to download them under -o\n" +
			"(the current directory by default), keeping their folders; -o - writes\n" +
			"one file to stdout. Nothing is overwritten.",
		Example: "  hkr findings evidence Fx7Kq2XpAb2m\n" +
			"  hkr findings evidence Fx7Kq2XpAb2m F-01/request.txt -o -\n" +
			"  hkr findings evidence Fx7Kq2XpAb2m --all -o evidence",
		Args: argsBetween(1, 100),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := args[1:]
			if all && len(paths) > 0 {
				return usageError{errors.New("name paths or pass --all, not both")}
			}
			if out == "-" && (all || len(paths) != 1) {
				return usageError{errors.New("-o - writes one file: name exactly one path")}
			}
			cl, org, id, err := a.finding(cmd, args[0])
			if err != nil {
				return err
			}
			page, err := api.List[api.Evidence](cmd.Context(), cl, api.FindingPath(org, id)+"/evidence", nil, true)
			if err != nil {
				return err
			}
			if !all && len(paths) == 0 {
				if a.flags.json {
					return a.printJSON(page.JSON(true))
				}
				return a.evidenceTable(id, page.Items)
			}
			// The newest scan's copy of each path.
			newest := map[string]api.Evidence{}
			var order []string
			for _, e := range page.Items {
				if _, ok := newest[e.Path]; !ok {
					newest[e.Path] = e
					order = append(order, e.Path)
				}
			}
			if all {
				paths = order
			}
			for _, p := range paths {
				e, ok := newest[p]
				if !ok {
					return fmt.Errorf("%s has no evidence file %s", id, render.Safe(p))
				}
				if err := a.saveEvidence(cmd, cl, org, id, e, out); err != nil {
					return err
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "download every file")
	c.Flags().StringVarP(&out, "output", "o", ".", "the directory to save under, or - for stdout")
	return c
}

func (a *app) evidenceTable(id string, files []api.Evidence) error {
	if len(files) == 0 {
		a.out("%s has no evidence files.", id)
		return nil
	}
	rows := make([][]string, 0, len(files))
	for _, e := range files {
		rows = append(rows, []string{render.Safe(e.Path), strconv.Itoa(e.Scan), size(e.Size)})
	}
	a.out("%s", render.Table([]string{"PATH", "SCAN", "SIZE"}, rows))
	return nil
}

func (a *app) saveEvidence(cmd *cobra.Command, cl *api.Client, org, id string, e api.Evidence, out string) error {
	rel, err := safeRelPath(e.Path)
	if err != nil {
		return err
	}
	path := api.FindingPath(org, id) + "/evidence/" + escapePath(e.Path)
	data, err := cl.Download(cmd.Context(), path, api.Query{"scan": strconv.Itoa(e.Scan)})
	if err != nil {
		return err
	}
	if out == "-" {
		_, err := a.env.Stdout.Write(data)
		return err
	}
	dest := filepath.Join(out, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // A path checked by safeRelPath, under the directory asked for.
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists; not overwriting it", filepath.ToSlash(dest))
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.progress("Saved %s", filepath.ToSlash(dest))
	return nil
}

// safeRelPath is a server-given evidence path as a local relative path,
// refusing anything that would land outside the output directory.
func safeRelPath(p string) (string, error) {
	local := filepath.FromSlash(p)
	if strings.HasPrefix(p, "/") || !filepath.IsLocal(local) || filepath.Clean(local) == "." {
		return "", fmt.Errorf("not saving evidence with an unsafe path %q", render.Safe(p))
	}
	return filepath.Clean(local), nil
}

// escapePath escapes each segment of a slash-separated path.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = api.Seg(s)
	}
	return strings.Join(parts, "/")
}

// size is a file size for people: 1.8 KB.
func size(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
