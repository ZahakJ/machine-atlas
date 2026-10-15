package server

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/ZahakJ/machine-atlas/fixtures"
	"github.com/ZahakJ/machine-atlas/web"
)

var (
	embeddedWeb      fs.FS = web.FS
	embeddedFixtures fs.FS = fixtures.FS
)

// contentTypes are the types the interface's files are served with. A file
// of any other extension is not served: the interface has no reason to
// ship one, and an unknown type would invite content sniffing.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

// static serves the interface's files from s.opt.Web. It never lists a
// directory, and a request path is turned into a file system path only
// through path.Clean and fs.ValidPath, so ".." cannot climb out (and an
// fs.FS cannot name anything outside itself anyway).
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	ctype, ok := contentTypes[path.Ext(name)]
	if !ok || !fs.ValidPath(name) || hidden(name) {
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	f, err := s.opt.Web.Open(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "cannot read file")
			return
		}
		content = bytes.NewReader(data)
	}
	w.Header().Set("Content-Type", ctype)
	// Revalidate on every load: the files change with the binary, or on
	// each edit under ATLAS_WEB_DIR.
	w.Header().Set("Cache-Control", "no-cache")
	// Embedded files have no modification time; ServeContent then sends no
	// Last-Modified, which is right.
	http.ServeContent(w, r, name, info.ModTime(), content)
}

// hidden reports whether any element of a slash-separated path starts with
// a dot (.keep files, editor droppings, .git under ATLAS_WEB_DIR).
func hidden(name string) bool {
	for _, el := range strings.Split(name, "/") {
		if strings.HasPrefix(el, ".") {
			return true
		}
	}
	return false
}
