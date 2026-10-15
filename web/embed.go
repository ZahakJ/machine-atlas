// Package web embeds the browser interface: index.html and the css/ and js/
// trees, served unchanged by internal/server. There is no build step; the
// files on disk are the files served. Development pages (dev/) and tests
// (test/) are deliberately not embedded.
package web

import "embed"

// FS holds index.html, css/ and js/ at those paths.
//
//go:embed index.html css js
var FS embed.FS
