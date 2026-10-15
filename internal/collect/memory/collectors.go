package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/catalog"
	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Collectors returns the memory collectors in the order they run.
func Collectors() []collect.Collector {
	return []collect.Collector{
		meminfoCollector(),
		vmstatCollector(),
		zoneinfoCollector(),
		buddyinfoCollector(),
		swapsCollector(),
		sysctlCollector(),
		thpCollector(),
		lruGenCollector(),
		zswapCollector(),
		zramCollector(),
		psiCollector(),
		numaCollector(),
		hugepagesCollector(),
		blockSizeCollector(),
		slabinfoCollector(),
		cgroupCollector(),
	}
}

// collector is one memory collector. The memory interfaces are small files
// read in microseconds, so read ignores the context: there is nothing to
// cancel between two reads that would return sooner.
type collector struct {
	id   string
	owns []string
	read func(env collect.Env) []obs.Observation
}

func (c collector) ID() string     { return c.id }
func (c collector) Owns() []string { return c.owns }
func (c collector) Collect(_ context.Context, env collect.Env) []obs.Observation {
	return c.read(env)
}

// declared returns the exact catalog ids under prefix ("mem.meminfo."): the
// fields lessons rely on, which a collector must account for even when the
// kernel does not print them.
func declared(prefix string) []string {
	var ids []string
	for _, e := range catalog.All() {
		if strings.HasPrefix(e.ID, prefix) {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// unavailable is the observation for id when reading src failed. ENOENT
// often means the kernel lacks the feature rather than that something is
// switched off; absentNote, when set, says which, ahead of the error text.
func unavailable(id string, src obs.Source, err error, absentNote string) obs.Observation {
	o := obs.Errored(id, src, err)
	if o.Status == obs.StatusAbsent && absentNote != "" {
		o.Note = absentNote + " (" + o.Note + ")"
	}
	return o
}

// unavailableAll applies unavailable to every id.
func unavailableAll(ids []string, src obs.Source, err error, absentNote string) []obs.Observation {
	out := make([]obs.Observation, len(ids))
	for i, id := range ids {
		out[i] = unavailable(id, src, err, absentNote)
	}
	return out
}

// malformed is the observation for content that did not parse.
func malformed(id string, src obs.Source, err error) obs.Observation {
	return obs.Failed(id, src, "unexpected format: "+err.Error())
}

// malformedAll applies malformed to every id.
func malformedAll(ids []string, src obs.Source, err error) []obs.Observation {
	out := make([]obs.Observation, len(ids))
	for i, id := range ids {
		out[i] = malformed(id, src, err)
	}
	return out
}

// errnoText describes err without the path it carries. Used where the path
// would repeat an identifying cgroup path in a note.
func errnoText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	return err.Error()
}

// parseInt parses a file holding one decimal integer, as sysctl and most
// sysfs attribute files do.
func parseInt(data []byte) (int64, error) {
	s := strings.TrimSpace(string(data))
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("expected one integer, got " + strconv.Quote(clipText(s)))
	}
	return n, nil
}

// kibToBytes converts a non-negative KiB count to bytes, refusing values
// that would overflow rather than wrapping.
func kibToBytes(n int64) (int64, error) {
	if n < 0 || n > math.MaxInt64/1024 {
		return 0, fmt.Errorf("%d KiB is out of range", n)
	}
	return n * 1024, nil
}

// clipText shortens s for an error message so a garbage file cannot produce
// an enormous note.
func clipText(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "") + "…"
}
