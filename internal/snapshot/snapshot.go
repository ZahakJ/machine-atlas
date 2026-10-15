// Package snapshot defines the versioned file format that carries a
// machine's observations: schema v1, its canonical encoding and content id,
// strict decoding and validation of untrusted files, and redaction.
//
// A snapshot holds observations only. Derived facts are recomputed from it,
// so the same snapshot and application version always yield the same
// lessons.
package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/collect"
	"github.com/ZahakJ/machine-atlas/internal/obs"
	"github.com/ZahakJ/machine-atlas/internal/version"
)

// Schema identifiers and limits (ARCHITECTURE.md §5).
const (
	Schema          = "machine-atlas/snapshot"
	SchemaVersion   = 1
	MaxBytes        = 8 << 20
	MaxObservations = 20000
)

// Snapshot is schema v1. Field order is the canonical encoding order.
type Snapshot struct {
	Schema        string            `json:"schema"`
	SchemaVersion int               `json:"schema_version"`
	AppVersion    string            `json:"app_version"`
	ID            string            `json:"id"`
	CreatedAt     string            `json:"created_at"`
	Collection    Collection        `json:"collection"`
	Redaction     Redaction         `json:"redaction"`
	Observations  []obs.Observation `json:"observations"`
}

// Collection describes how the observations were gathered.
type Collection struct {
	Mode       string         `json:"mode"` // "live" | "fixture"
	Fixture    string         `json:"fixture"`
	Privileged bool           `json:"privileged"`
	StartedAt  string         `json:"started_at"`
	FinishedAt string         `json:"finished_at"`
	Collectors []CollectorRun `json:"collectors"`
}

// CollectorRun is one collector's line in the run report.
type CollectorRun struct {
	ID           string `json:"id"`
	Status       string `json:"status"` // "ok" | "partial" | "failed" | "skipped"
	DurationMS   int64  `json:"duration_ms"`
	Observations int    `json:"observations"`
	Error        string `json:"error"`
}

// Redaction records what redaction was applied to the snapshot.
type Redaction struct {
	Applied  bool     `json:"applied"`
	Policy   string   `json:"policy"` // "none" | "default" | "strict"
	Classes  []string `json:"classes"`
	Replaced int      `json:"replaced"`
}

// Build assembles an unredacted snapshot from a collection run and seals it
// with its content id.
func Build(rep collect.Report, observations []obs.Observation) *Snapshot {
	s := &Snapshot{
		Schema:        Schema,
		SchemaVersion: SchemaVersion,
		AppVersion:    version.Version,
		CreatedAt:     obs.Timestamp(rep.FinishedAt),
		Collection: Collection{
			Mode:       rep.Mode,
			Fixture:    rep.Fixture,
			Privileged: rep.Privileged,
			StartedAt:  obs.Timestamp(rep.StartedAt),
			FinishedAt: obs.Timestamp(rep.FinishedAt),
			Collectors: make([]CollectorRun, len(rep.Collectors)),
		},
		Redaction:    Redaction{Policy: PolicyNone, Classes: []string{}},
		Observations: slices.Clone(observations),
	}
	for i, c := range rep.Collectors {
		s.Collection.Collectors[i] = CollectorRun{
			ID: c.ID, Status: c.Status, DurationMS: c.Duration.Milliseconds(),
			Observations: c.Observations, Error: c.Error,
		}
	}
	s.Seal()
	return s
}

// Seal sorts the observations and sets the content id.
func (s *Snapshot) Seal() {
	sortObservations(s.Observations)
	s.ID = ComputeID(s)
}

func sortObservations(o []obs.Observation) {
	slices.SortStableFunc(o, func(a, b obs.Observation) int { return strings.Compare(a.ID, b.ID) })
}

// Encode returns the canonical encoding: fields in struct order, observations
// sorted by id, two-space indent, a trailing newline, and no HTML escaping
// (placeholders such as <redacted:hostname> stay readable).
func Encode(s *Snapshot) ([]byte, error) {
	c := *s
	c.Observations = slices.Clone(s.Observations)
	sortObservations(c.Observations)
	if c.Redaction.Classes == nil {
		c.Redaction.Classes = []string{}
	}
	if c.Collection.Collectors == nil {
		c.Collection.Collectors = []CollectorRun{}
	}
	if c.Observations == nil {
		c.Observations = []obs.Observation{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ComputeID returns "sha256:" and the hex SHA-256 of the canonical encoding
// with the id empty and every duration zeroed, so re-running the same
// fixture, or re-encoding a decoded file, yields the same id.
func ComputeID(s *Snapshot) string {
	c := *s
	c.ID = ""
	c.Collection.Collectors = slices.Clone(s.Collection.Collectors)
	for i := range c.Collection.Collectors {
		c.Collection.Collectors[i].DurationMS = 0
	}
	data, err := Encode(&c)
	if err != nil {
		// Only unencodable values (channels, functions) fail, and Validate
		// rejects those; an id that cannot match any content says so.
		return "sha256:invalid"
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ErrTooLarge reports an input over MaxBytes.
var ErrTooLarge = fmt.Errorf("snapshot exceeds %d bytes", MaxBytes)

// Decode reads one snapshot from r. It reads at most MaxBytes (so a hostile
// file cannot make atlas allocate more), rejects unknown fields and trailing
// data, and normalizes values to the types collection produces. It does not
// validate; call Validate, or use Load for both.
func Decode(r io.Reader) (*Snapshot, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, ErrTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("decode snapshot: unexpected data after the snapshot object")
	}
	for i := range s.Observations {
		// A value that cannot be normalized is left as decoded, so Validate
		// can say precisely what is wrong with it.
		if v, err := obs.Normalize(s.Observations[i].Value); err == nil {
			s.Observations[i].Value = v
		}
	}
	return &s, nil
}

// Load decodes and validates a snapshot, returning every validation problem
// joined into one error.
func Load(r io.Reader) (*Snapshot, error) {
	s, err := Decode(r)
	if err != nil {
		return nil, err
	}
	if errs := Validate(s); len(errs) > 0 {
		return nil, fmt.Errorf("invalid snapshot: %w", errors.Join(errs...))
	}
	return s, nil
}
