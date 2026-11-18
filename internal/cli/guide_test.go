package cli

import (
	"strings"
	"testing"
)

// Every hkr command the guide shows is one this version has, with flags
// it takes.
func TestGuideNamesOnlyRealCommands(t *testing.T) {
	inCode := false
	n := 0
	for _, line := range strings.Split(guideText, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		line, _, _ = strings.Cut(strings.TrimSpace(line), "#")
		line = strings.TrimSpace(line)
		if !inCode || !strings.HasPrefix(line, "hkr ") {
			continue
		}
		n++
		args := strings.Fields(strings.TrimPrefix(line, "hkr "))
		root := newRootCmd(&app{env: withDefaults(Env{})})
		cmd, rest, err := root.Find(args)
		if err != nil || cmd == root {
			t.Errorf("guide: %q isn't a command", line)
			continue
		}
		if err := cmd.ParseFlags(rest); err != nil {
			t.Errorf("guide: %q: %v", line, err)
		}
		runsWithoutUsageError(t, line)
	}
	if !strings.Contains(guideText, "Next:") {
		t.Error("the guide must say the command to run is on hkr status's Next: line; --json has no commands")
	}
	if n < 5 {
		t.Errorf("guide shows %d commands; expected the whole setup", n)
	}
}
