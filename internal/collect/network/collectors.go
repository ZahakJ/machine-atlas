// Package network collects what an unprivileged process can read about the
// machine's network stack: interfaces and their queues from /sys/class/net,
// addresses (/proc/net/if_inet6 and `ip -json`), routes, socket counts,
// protocol counters, per-CPU receive statistics, queueing disciplines
// (`tc -json`), connection tracking and the net.* sysctls.
//
// Privacy: nothing about other processes' connections is kept. The socket
// tables are read only to count sockets by state; addresses, ports, owners
// and inodes are never stored. Addresses of this machine's own interfaces
// and routes are collected, and the catalog marks those cells so every
// redaction policy replaces them. Interface names never appear in a source
// path or a note: per-interface failures are reported without the name.
package network

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collectors returns the network collectors in the order they run: what
// the machine is attached to, then what flows through it.
func Collectors() []collect.Collector {
	return []collect.Collector{
		interfacesCollector,
		ipv4AddrCollector,
		ipv6AddrCollector,
		routesCollector,
		sockstatCollector,
		socketsCollector,
		snmpCollector,
		softnetCollector,
		qdiscCollector,
		conntrackCollector,
		sysctlCollector(),
	}
}

// collector adapts a function to collect.Collector.
type collector struct {
	id   string
	owns []string
	run  func(ctx context.Context, env collect.Env) []obs.Observation
}

func (c collector) ID() string     { return c.id }
func (c collector) Owns() []string { return c.owns }
func (c collector) Collect(ctx context.Context, env collect.Env) []obs.Observation {
	return c.run(ctx, env)
}

// unavailable is the observation for id when reading src failed; absentNote,
// when set, says what a missing file means here, ahead of the error.
func unavailable(id string, src obs.Source, err error, absentNote string) obs.Observation {
	o := obs.Errored(id, src, err)
	if o.Status == obs.StatusAbsent && absentNote != "" {
		o.Note = absentNote + " (" + o.Note + ")"
	}
	return o
}

// malformed is the observation for content that did not parse.
func malformed(id string, src obs.Source, err error) obs.Observation {
	return obs.Failed(id, src, "unexpected format: "+err.Error())
}

// errnoText describes err without the path it carries, for failures under
// /sys/class/net/<name>, whose path would name the interface.
func errnoText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	return err.Error()
}

// readTrim reads a small text file and trims surrounding whitespace.
func readTrim(env collect.Env, path string) (string, error) {
	data, err := env.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// parseInt parses a file holding one decimal integer.
func parseInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("expected one integer, got " + strconv.Quote(clip(s)))
	}
	return n, nil
}

// clip shortens s for an error message.
func clip(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + "…"
}

// rejectsOption recognises a tool that does not understand the JSON option
// it was given: iproute2 before JSON support ('Option "-json" is unknown')
// and busybox applets (which print their usage).
func rejectsOption(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "is unknown") || strings.Contains(s, "unrecognized option") ||
		strings.Contains(s, "invalid option") || strings.Contains(s, "unknown option") || strings.Contains(s, "usage:")
}

// runJSON runs a command expected to print JSON and turns its failures into
// statuses: absent when the tool is missing, unsupported when it rejects
// the JSON option. ok is false when the returned observation is final.
func runJSON(ctx context.Context, env collect.Env, id, missingNote, oldNote string, argv ...string) ([]byte, obs.Observation, bool) {
	src := obs.Command(argv...)
	out, err := env.Run(ctx, argv[0], argv[1:]...)
	var exit *collect.ExitError
	switch {
	case errors.As(err, &exit) && rejectsOption(exit.Stderr):
		return nil, obs.Unsupported(id, src, oldNote+": "+clip(firstLine(exit.Stderr))), false
	case errors.As(err, &exit) && strings.Contains(exit.Stderr, "Permission denied"):
		return nil, obs.Denied(id, src, strings.TrimSpace(exit.Error())), false
	case err != nil:
		o := obs.Errored(id, src, err)
		if o.Status == obs.StatusAbsent {
			o.Note = missingNote
		}
		return nil, o, false
	}
	return out, obs.Observation{}, true
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
