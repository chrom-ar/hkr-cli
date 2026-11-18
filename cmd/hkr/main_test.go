package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain makes the test binary also act as hkr for the scripts.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"hkr": func() { os.Exit(run(os.Args[1:])) },
	})
}

// TestScripts runs hkr through every script in testdata/script,
// asserting output, exit codes and files.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"exits": exits,
		},
	})
}

// exits runs a program and asserts its exit code:
//
//	exits 2 hkr --bogus
//
// stdout and stderr are kept for the stdout and stderr commands.
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exits <code> <program> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exits: bad code %q", args[0])
	}
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			ts.Fatalf("exits: running %s: %v", args[1], err)
		}
		got = ee.ExitCode()
	}
	if got != want {
		ts.Fatalf("exit code = %d, want %d", got, want)
	}
}
