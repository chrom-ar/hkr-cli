package cli

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
)

// confirm asks before spending or stopping something: --yes skips the
// question; without a terminal to ask at, it's a usage mistake whose
// words say what --yes would do.
func (a *app) confirm(question, noTerminal string) error {
	if a.flags.yes {
		return nil
	}
	if !a.env.StdinIsTerminal {
		return usageError{errors.New(noTerminal)}
	}
	fmt.Fprintf(a.env.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(a.env.Stdin).ReadString('\n')
	if ans := strings.ToLower(strings.TrimSpace(line)); ans == "y" || ans == "yes" {
		return nil
	}
	fmt.Fprintln(a.env.Stderr, "Nothing changed.")
	return silentError{ExitError}
}
