// Command atlas is Machine Atlas: an interactive textbook of the Linux
// machine it runs on. This file only dispatches; each verb lives in its own
// cmd_<verb>.go file and the work itself in internal/.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ZahakJ/machine-atlas/internal/collect/probe"
	"github.com/ZahakJ/machine-atlas/internal/experiment"
)

// verb is one subcommand. Hidden verbs are the child-process entry points
// atlas re-executes itself with; they are not for people to type.
type verb struct {
	name    string
	summary string
	run     func(args []string) int
	hidden  bool
}

var verbs = []verb{
	{name: "serve", summary: "serve the interface on 127.0.0.1", run: cmdServe},
	{name: "collect", summary: "collect a snapshot of this machine or a fixture root", run: cmdCollect},
	{name: "snapshot", summary: "validate, preview, redact or show a snapshot file", run: cmdSnapshot},
	{name: "lesson", summary: "print a lesson bound to a snapshot", run: cmdLesson},
	{name: "curriculum", summary: "check the curriculum (lint)", run: cmdCurriculum},
	{name: "experiment", summary: "list or run an opt-in experiment", run: cmdExperiment},
	{name: "fixtures", summary: "list the embedded demonstration snapshots", run: cmdFixtures},
	{name: "version", summary: "print version information", run: cmdVersion},
	{name: "__probe", run: probe.ChildMain, hidden: true},
	{name: "__experiment", run: experiment.ChildMain, hidden: true},
}

func main() { os.Exit(run(os.Args[1:])) }

// run dispatches args to a verb and returns the exit code: 0 success,
// 1 failure, 2 usage error.
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		usage(os.Stdout)
		return 0
	}
	for _, v := range verbs {
		if v.name == args[0] {
			return v.run(args[1:])
		}
	}
	fmt.Fprintf(os.Stderr, "atlas: unknown command %q\n\n", args[0])
	usage(os.Stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprint(w, "Machine Atlas: an interactive textbook of the Linux machine it runs on.\n\nUsage: atlas <command> [arguments]\n\nCommands:\n")
	for _, v := range verbs {
		if !v.hidden {
			fmt.Fprintf(w, "  %-11s %s\n", v.name, v.summary)
		}
	}
	fmt.Fprint(w, "\nRun \"atlas <command> -h\" for a command's options.\n")
}

// newFlags returns a flag set that reports errors instead of exiting, with
// synopsis printed above the option list on -h.
func newFlags(name, synopsis string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

// parseArgs parses flags that may appear before, between or after
// positional arguments ("atlas snapshot redact FILE -o OUT"), which the flag
// package alone stops at. It returns the positional arguments and an exit
// code: -1 to continue, 0 after -h, 2 after a usage error.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, int) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, 0
			}
			return nil, 2
		}
		if fs.NArg() == 0 {
			return positional, -1
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// fail prints an error for verb and returns exit code 1.
func fail(verb string, err error) int {
	fmt.Fprintf(os.Stderr, "atlas %s: %v\n", verb, err)
	return 1
}

// usageError prints a usage problem for verb and returns exit code 2.
func usageError(fs *flag.FlagSet, format string, args ...any) int {
	fmt.Fprintf(fs.Output(), "atlas %s: %s\n", fs.Name(), fmt.Sprintf(format, args...))
	fs.Usage()
	return 2
}
