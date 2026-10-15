package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestHostGuard(t *testing.T) {
	h := newHarness(t, ModeFixture)
	for _, host := range []string{"127.0.0.1:7373", "localhost:7373", "LOCALHOST:7373", "[::1]:7373", "127.0.0.2:7373"} {
		if rec := h.do(req{path: "/api/state", host: host}); rec.Code != http.StatusOK {
			t.Errorf("Host %q refused: %d", host, rec.Code)
		}
	}
	// Names that DNS could point here (rebinding), the wrong port, no port,
	// and non-loopback addresses are all refused, for pages as well as API.
	for _, host := range []string{"evil.example:7373", "evil.example", "127.0.0.1:9999", "127.0.0.1",
		"localhost.evil.example:7373", "192.168.1.10:7373", "[fe80::1%25eth0]:7373", "127.0.0.1:7373:7373"} {
		for _, path := range []string{"/api/state", "/"} {
			rec := h.do(req{path: path, host: host})
			wantError(t, rec, http.StatusMisdirectedRequest, "bad_host")
			if strings.Contains(rec.Body.String(), h.Token()) {
				t.Errorf("Host %q: token leaked", host)
			}
			if rec.Header().Get("Content-Security-Policy") != csp {
				t.Errorf("Host %q: refusal without CSP", host)
			}
		}
	}
}

func TestHostGuardAllowRemote(t *testing.T) {
	h := newHarness(t, ModeFixture)
	h.opt.AllowRemote = true
	if rec := h.do(req{path: "/api/state", host: "192.168.1.10:7373"}); rec.Code != http.StatusOK {
		t.Errorf("IP literal refused with AllowRemote: %d", rec.Code)
	}
	// An IP literal cannot be rebound; a name still can.
	wantError(t, h.do(req{path: "/api/state", host: "evil.example:7373"}), http.StatusMisdirectedRequest, "bad_host")
}

func TestTokenGuard(t *testing.T) {
	h := newHarness(t, ModeLive)
	empty, wrong, short := "", strings.Repeat("0", 64), h.Token()[:10]
	for _, path := range []string{"/api/collect", "/api/snapshot/live", "/api/snapshot/import",
		"/api/snapshot/preview", "/api/fixtures/restricted/activate", "/api/experiments/fault-count/run"} {
		for _, tok := range []*string{&empty, &wrong, &short} {
			r := req{method: http.MethodPost, path: path, body: []byte(`{"acknowledged":true}`), token: tok}
			wantError(t, h.do(r), http.StatusForbidden, "bad_token")
		}
	}
	if h.collects.Load() != 0 || h.runs.Load() != 0 || h.state().Mode != ModeLive {
		t.Error("a refused request changed state")
	}
}

func TestOriginGuard(t *testing.T) {
	h := newHarness(t, ModeFixture)
	for _, origin := range []string{"http://evil.example", "null", "http://127.0.0.1:9999", "https://127.0.0.1:7373", "http://localhost:7373"} {
		wantError(t, h.do(req{method: http.MethodPost, path: "/api/fixtures/restricted/activate", origin: origin}),
			http.StatusForbidden, "bad_origin")
	}
	if h.state().Fixture != "desktop-x86_64" {
		t.Fatal("a cross-origin request changed the snapshot")
	}
	var st stateResponse
	decode(t, h.do(req{method: http.MethodPost, path: "/api/fixtures/restricted/activate", origin: "http://" + testHost}),
		http.StatusOK, &st)
	if st.Fixture != "restricted" {
		t.Errorf("same-origin request: %+v", st)
	}
}

func TestBodyCaps(t *testing.T) {
	h := newHarness(t, ModeLive)
	big := `{"policy":"default","pad":"` + strings.Repeat("x", maxBody) + `"}`
	wantError(t, h.post("/api/snapshot/preview", big), http.StatusRequestEntityTooLarge, "body_too_large")
	wantError(t, h.post("/api/experiments/fault-count/run", big), http.StatusRequestEntityTooLarge, "body_too_large")
	if h.runs.Load() != 0 {
		t.Error("an oversized request ran an experiment")
	}
	wantError(t, h.post("/api/snapshot/preview", `{"policy":"default"} {"policy":"none"}`), http.StatusBadRequest, "bad_request")
	wantError(t, h.post("/api/snapshot/preview", `[1,2]`), http.StatusBadRequest, "bad_request")
}
