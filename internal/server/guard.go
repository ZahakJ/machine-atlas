package server

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// Body limits (ARCHITECTURE.md §10). Imports carry a whole snapshot; every other
// request body is a small JSON object.
const (
	maxImportBody = 8 << 20
	maxBody       = 64 << 10
)

// csp is the content security policy of ARCHITECTURE.md §9. The interface uses
// no inline script or style, so nothing needs to be relaxed.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// tokenHeader carries the per-process token on state-changing requests.
const tokenHeader = "X-Atlas-Token"

// guard wraps every request: security headers first (so even refusals carry
// them), then the Host check, then, for methods that change state, the
// token and Origin checks, and finally a body limit.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// Answers describe the active snapshot, which changes.
			h.Set("Cache-Control", "no-store")
		}

		if !s.hostAllowed(r.Host) {
			// A page served from another name that resolves here (DNS
			// rebinding) arrives with that name in Host.
			writeError(w, http.StatusMisdirectedRequest, "bad_host",
				"this server answers only to localhost, 127.0.0.1 or [::1] on port "+strconv.Itoa(s.opt.Port))
			return
		}
		if !safeMethod(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
				writeError(w, http.StatusForbidden, "bad_origin", "cross-origin requests are refused")
				return
			}
			got := r.Header.Get(tokenHeader)
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				writeError(w, http.StatusForbidden, "bad_token",
					"state-changing requests need the "+tokenHeader+" header from GET /api/state")
				return
			}
		}
		limit := int64(maxBody)
		if r.URL.Path == "/api/snapshot/import" {
			limit = maxImportBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

// hostAllowed reports whether a Host header names this server: localhost or
// a loopback IP literal (any IP literal with AllowRemote), with the bound
// port. The port is required: browsers always send it for a non-default
// port, and a Host without it did not come from a page on this server.
func (s *Server) hostAllowed(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil || port != strconv.Itoa(s.opt.Port) {
		return false
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(name)
	if err != nil || ip.Zone() != "" {
		return false
	}
	return ip.IsLoopback() || s.opt.AllowRemote
}
