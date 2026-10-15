// Package curriculum embeds the authored curriculum: the lessons
// (lessons/*.md), the roadmap (roadmap.json) and the Explore component graph
// (components.json). internal/curriculum parses and binds them; this package
// only carries the files into the binary.
package curriculum

import "embed"

// FS holds lessons/ and the top-level JSON files, at those paths. The
// lessons directory is embedded with all: so it compiles while it holds only
// its .keep placeholder; readers should match lessons/*.md.
//
//go:embed all:lessons *.json
var FS embed.FS
