package cli

import (
	"path/filepath"
	"testing"
)

func TestSafeRelPath(t *testing.T) {
	for _, ok := range []string{"F-01/request.txt", "a.png", "a/../b.txt", "x/./y"} {
		if _, err := safeRelPath(ok); err != nil {
			t.Errorf("safeRelPath(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "a/..", "..", "../x", "a/../../x", "/etc/passwd"} {
		if got, err := safeRelPath(bad); err == nil {
			t.Errorf("safeRelPath(%q) = %q, want refused", bad, got)
		}
	}
	// Whatever is accepted stays under the output directory.
	for _, p := range []string{`..\x`, "C:/x", "a/b/../../c"} {
		if got, err := safeRelPath(p); err == nil && !filepath.IsLocal(got) {
			t.Errorf("safeRelPath(%q) = %q, not local", p, got)
		}
	}
}

func TestSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1832: "1.8 KB", 20480: "20.0 KB", 3 << 20: "3.0 MB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}
