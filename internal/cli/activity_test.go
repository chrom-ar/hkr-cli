package cli

import (
	"encoding/json"
	"testing"

	"github.com/chrom-ar/hkr-cli/internal/api"
)

func TestActivityText(t *testing.T) {
	for want, in := range map[string]string{
		"push main a3f21c9d8e7b by ana → skipped (branch_not_reviewed)": `{"kind":"delivery","event":"push","branch":"main",
			"commit_sha":"a3f21c9d8e7b6a5f","pushed_by":"ana","outcome":"skipped","reason":"branch_not_reviewed","review":null}`,
		"pull_request feat a3f21c9d8e7b → reviewed (review running)": `{"kind":"delivery","event":"pull_request","branch":"feat",
			"commit_sha":"a3f21c9d8e7b6a5f","outcome":"reviewed","review":{"status":"running"}}`,
		"PR #412 open, verified · fixes F-07": `{"kind":"pull_request","number":412,"state":"open","status":"verified",
			"finding":{"ref":"F-07","title":"x"},"dependency":null}`,
		"PR #9 merged · upgrades lodash":          `{"kind":"pull_request","number":9,"state":"merged","dependency":{"package_name":"lodash"}}`,
		"lock files main a1b2c3d4e5f6 · 2/3 read": `{"kind":"dependencies","status":"complete","branch":"main","commit_sha":"a1b2c3d4e5f6a1b2","projects_read":2,"projects_unread":1}`,
		"lock files main · failed":                `{"kind":"dependencies","status":"failed","branch":"main"}`,
		// Server text is neutralized.
		"push main by \uFFFD[31mana → received": `{"kind":"delivery","event":"push","branch":"main","pushed_by":"\u001b[31mana","outcome":"received"}`,
	} {
		var e api.Activity
		if err := json.Unmarshal([]byte(in), &e); err != nil {
			t.Fatal(err)
		}
		if got := activityText(&e); got != want {
			t.Errorf("activityText(%s) = %q, want %q", in, got, want)
		}
	}
}

// bumpedText is what HKr read back from the lock file after an upgrade,
// each version once.
func TestBumpedText(t *testing.T) {
	for want, vs := range map[string][]string{
		"":                        nil,
		"lodash 4.17.21":          {"4.17.21", "4.17.21"},
		"lodash 4.17.21, 4.17.20": {"4.17.21", "4.17.20", "4.17.21"},
	} {
		if got := bumpedText("lodash", vs); got != want {
			t.Errorf("bumpedText(%q) = %q, want %q", vs, got, want)
		}
	}
}

// A failed read says where it stopped.
func TestFailedReadText(t *testing.T) {
	var e api.Activity
	if err := json.Unmarshal([]byte(`{"kind":"dependencies","status":"failed","branch":"main","failure_reason":"clone"}`), &e); err != nil {
		t.Fatal(err)
	}
	if got, want := activityText(&e), "lock files main · failed (clone)"; got != want {
		t.Errorf("activityText = %q, want %q", got, want)
	}
	for reason, want := range map[string]string{
		"container":      "HKr could not start the machine that reads the repository.",
		"clone":          "HKr could not download the repository. If this keeps happening, check that its connection still has access.",
		"workspace":      "The reader could not get started.",
		"agent":          "The reader could not get started.",
		"never_finished": "The reader stopped before it finished.",
		"":               "",
		"new_reason":     "new_reason",
	} {
		if got := failureText(reason); got != want {
			t.Errorf("failureText(%q) = %q, want %q", reason, got, want)
		}
	}
}
