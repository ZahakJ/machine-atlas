package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ZahakJ/machine-atlas/internal/curriculum"
	"github.com/ZahakJ/machine-atlas/internal/experiment"
)

const experimentSynopsis = "atlas experiment list | run ID [--yes] [--json]"

func cmdExperiment(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return experimentCLI{
		in: os.Stdin, out: os.Stdout, errOut: os.Stderr,
		tty: experiment.IsTerminal(os.Stdin),
		run: func(id string) (experiment.Result, error) { return experiment.Run(ctx, id) },
	}.main(args)
}

// experimentCLI is "atlas experiment" with its streams and the runner
// injected, so the confirmation rules can be tested without running
// anything.
type experimentCLI struct {
	in          io.Reader
	out, errOut io.Writer
	tty         bool
	run         func(id string) (experiment.Result, error)
}

func (c experimentCLI) main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.errOut, "Usage:", experimentSynopsis)
		return 2
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			fmt.Fprintln(c.errOut, "Usage: atlas experiment list")
			return 2
		}
		return c.list()
	case "run":
		return c.runOne(args[1:])
	case "-h", "-help", "--help":
		fmt.Fprintln(c.out, "Usage:", experimentSynopsis)
		return 0
	}
	fmt.Fprintf(c.errOut, "atlas experiment: unknown subcommand %q\nUsage: %s\n", args[0], experimentSynopsis)
	return 2
}

func (c experimentCLI) list() int {
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tUSES\tSTATE")
	for _, in := range experiment.List() {
		state := "available"
		if !in.Available {
			state = "unavailable: " + in.UnavailableReason
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", in.ID, in.Title, resourceSummary(in.Resources), state)
	}
	if err := tw.Flush(); err != nil {
		return fail("experiment", err)
	}
	return 0
}

func (c experimentCLI) runOne(args []string) int {
	fs := newFlags("experiment", "atlas experiment run ID [--yes] [--json]")
	fs.SetOutput(c.errOut)
	yes := fs.Bool("yes", false, "run without asking for confirmation")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	pos, code := parseArgs(fs, args)
	if code >= 0 {
		return code
	}
	if len(pos) != 1 {
		return usageError(fs, "expected one experiment id")
	}
	info, ok := findExperiment(pos[0])
	if !ok {
		return usageError(fs, "no experiment %q; \"atlas experiment list\" shows them", pos[0])
	}
	if !info.Available {
		fmt.Fprintf(c.errOut, "atlas experiment: %s is unavailable: %s\n", info.ID, info.UnavailableReason)
		return 1
	}

	// The description goes where the person reads; with --json, stdout
	// carries only the result.
	w := c.out
	if *asJSON {
		w = c.errOut
	}
	describe(w, info)
	if !*yes {
		if !c.tty {
			fmt.Fprintln(c.errOut, "atlas experiment: not running: standard input is not a terminal, so nobody can confirm; pass --yes to run it anyway")
			return 1
		}
		fmt.Fprint(w, "\nRun this experiment? [y/N] ")
		answer, _ := bufio.NewReader(c.in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(w, "Not run.")
			return 1
		}
	}

	res, err := c.run(info.ID)
	if err != nil {
		return fail("experiment", err)
	}
	if *asJSON {
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fail("experiment", err)
		}
		return 0
	}
	report(c.out, info, res)
	return 0
}

func findExperiment(id string) (experiment.Info, bool) {
	for _, in := range experiment.List() {
		if in.ID == id {
			return in, true
		}
	}
	return experiment.Info{}, false
}

func resourceSummary(r experiment.Resources) string {
	return fmt.Sprintf("%s memory, %s disk, at most %d s",
		curriculum.Format(r.MemoryBytes, "bytes", "auto"), curriculum.Format(r.DiskBytes, "bytes", "auto"), r.TimeBoundSeconds)
}

// describe prints what the experiment asks, does, uses and does not do.
func describe(w io.Writer, in experiment.Info) {
	fmt.Fprintf(w, "%s (%s)\n\n%s\n\nWhat it does:\n", in.Title, in.ID, in.Question)
	for i, s := range in.Steps {
		fmt.Fprintf(w, "  %d. %s\n", i+1, s)
	}
	fmt.Fprintf(w, "\nResources: %s.\n\nWhat it does not do:\n", resourceSummary(in.Resources))
	for _, s := range in.DoesNot {
		fmt.Fprintf(w, "  - %s\n", s)
	}
}

// report prints the measurements, the matched outcome with its
// interpretation, the notes (what the numbers cannot establish, and the
// rule that chose the outcome), and the other outcomes briefly.
func report(w io.Writer, in experiment.Info, res experiment.Result) {
	fmt.Fprintf(w, "\nMeasurements (%s to %s):\n\n", res.StartedAt, res.FinishedAt)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME (UTC)\tSTEP\tMEASUREMENT\tVALUE")
	for _, m := range res.Measurements {
		at := m.At
		if t, err := time.Parse(time.RFC3339Nano, m.At); err == nil {
			at = t.UTC().Format("15:04:05.000000")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", at, m.Step, m.Label, formatMeasurement(m))
	}
	tw.Flush()

	for _, o := range in.Outcomes {
		if o.ID == res.Outcome {
			fmt.Fprintf(w, "\nOutcome: %s (%s)\n\n%s\n", o.Title, o.ID, o.Interpretation)
		}
	}
	if len(res.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, n := range res.Notes {
			fmt.Fprintf(w, "  - %s\n", n)
		}
	}
	fmt.Fprintln(w, "\nOther possible outcomes:")
	for _, o := range in.Outcomes {
		if o.ID != res.Outcome {
			fmt.Fprintf(w, "  - %s: %s\n", o.ID, o.Title)
		}
	}
}

// formatMeasurement shows a value readably, with the exact byte count
// beside a rounded size.
func formatMeasurement(m experiment.Measurement) string {
	s := curriculum.Format(m.Value, m.Unit, "auto")
	if n, ok := m.Value.(float64); ok && m.Unit == "bytes" && n >= 1024 {
		s += " (" + strconv.FormatFloat(n, 'f', -1, 64) + " B)"
	}
	return s
}
