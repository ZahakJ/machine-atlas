// Package fixtures embeds the golden demonstration snapshots
// (snapshots/<name>.json), generated from the fake machine trees in roots/ by
// "make fixtures", plus live-sample.json, a redacted capture of a real
// machine. Of the roots themselves only _meta.json is embedded, for its
// description; the rest of each tree stays out of the binary, since the
// snapshots are all that is needed to demonstrate the interface anywhere.
package fixtures

import "embed"

// FS holds snapshots/*.json and roots/*/_meta.json at those paths. The
// snapshots directory is embedded with all:, so it also holds a .keep
// file; readers should match snapshots/*.json.
//
//go:embed all:snapshots roots/*/_meta.json
var FS embed.FS
