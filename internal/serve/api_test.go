package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/keep"
)

// fakeOrch is a test double for the orchestrator seam.
type fakeOrch struct {
	services  []config.Service
	logDir    string
	calls     []string
	verbErr   error
	updating  bool  // UpdateInProgress answer
	updateErr error // Update failure to inject
}

func (f *fakeOrch) Targets(names []string) ([]*config.Service, error) {
	if len(names) == 0 {
		out := make([]*config.Service, 0, len(f.services))
		for i := range f.services {
			out = append(out, &f.services[i])
		}
		return out, nil
	}
	var out []*config.Service
	for _, n := range names {
		found := false
		for i := range f.services {
			if f.services[i].Name == n {
				out = append(out, &f.services[i])
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown service %q", n)
		}
	}
	return out, nil
}

func (f *fakeOrch) Status(names []string) ([]keep.ServiceStatus, error) {
	targets, err := f.Targets(names)
	if err != nil {
		return nil, err
	}
	out := make([]keep.ServiceStatus, 0, len(targets))
	for _, s := range targets {
		out = append(out, keep.ServiceStatus{Name: s.Name, Health: keep.HealthRunning})
	}
	return out, nil
}

func (f *fakeOrch) Up(s *config.Service) error {
	f.calls = append(f.calls, "up "+s.Name)
	return f.verbErr
}

func (f *fakeOrch) Down(s *config.Service) error {
	f.calls = append(f.calls, "down "+s.Name)
	return f.verbErr
}

func (f *fakeOrch) Bounce(s *config.Service) error {
	f.calls = append(f.calls, "bounce "+s.Name)
	return f.verbErr
}

func (f *fakeOrch) Update(ctx context.Context, s *config.Service, out io.Writer) (keep.UpdateResult, error) {
	f.calls = append(f.calls, "update "+s.Name)
	fmt.Fprintf(out, "==> update %s\nupdating...\n", s.Name)
	if f.updateErr != nil {
		return keep.UpdateResult{Service: s.Name, Error: f.updateErr.Error()}, f.updateErr
	}
	return keep.UpdateResult{Service: s.Name, OK: true}, nil
}

func (f *fakeOrch) UpdateInProgress(s *config.Service) bool { return f.updating }

func (f *fakeOrch) Show(name string) (keep.Resolved, error) {
	if _, err := f.Targets([]string{name}); err != nil {
		return keep.Resolved{}, err
	}
	return keep.Resolved{Name: name, Argv: []string{"/bin/true"}}, nil
}

func (f *fakeOrch) ComputePlan() (keep.Plan, error) { return keep.Plan{}, nil }
func (f *fakeOrch) Doctor() ([]keep.Finding, error) { return nil, nil }

func (f *fakeOrch) LogTargets(names []string) ([]keep.LogTarget, error) {
	targets, err := f.Targets(names)
	if err != nil {
		return nil, err
	}
	var out []keep.LogTarget
	for _, s := range targets {
		out = append(out,
			keep.LogTarget{Service: s.Name, Stream: "out", Path: filepath.Join(f.logDir, s.Name+".out.log")},
			keep.LogTarget{Service: s.Name, Stream: "err", Path: filepath.Join(f.logDir, s.Name+".err.log")},
		)
	}
	return out, nil
}

func testServer(t *testing.T, orch orchestrator, selfService string) *Server {
	t.Helper()
	s, err := New(Options{
		Password:    "hunter2",
		Token:       "sekrit-token",
		SelfService: selfService,
		Version:     "test",
		StatePath:   filepath.Join(t.TempDir(), "state.json"),
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.newOrch = func() (orchestrator, error) { return orch, nil }
	return s
}

func fakeWith(names ...string) *fakeOrch {
	f := &fakeOrch{}
	for _, n := range names {
		f.services = append(f.services, config.Service{Name: n})
	}
	return f
}

func do(h http.Handler, method, path string, body string, hdr map[string]string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var bearer = map[string]string{"Authorization": "Bearer sekrit-token"}

func TestUnauthenticatedRejected(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()
	for _, path := range []string{"/api/v1/services", "/api/v1/meta", "/api/v1/diff", "/api/v1/doctor", "/api/v1/auth/passkeys"} {
		if rec := do(h, "GET", path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s unauthenticated = %d, want 401", path, rec.Code)
		}
	}
	if rec := do(h, "POST", "/api/v1/services/opencode/bounce", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated bounce = %d, want 401", rec.Code)
	}
}

func TestBearerTokenAuth(t *testing.T) {
	h := testServer(t, fakeWith("opencode", "tailscaled"), "").Handler()
	rec := do(h, "GET", "/api/v1/services", "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /services with bearer = %d, want 200", rec.Code)
	}
	var body struct {
		Services []keep.ServiceStatus `json:"services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Services) != 2 {
		t.Fatalf("got %d services, want 2", len(body.Services))
	}
	if rec := do(h, "GET", "/api/v1/services", "", map[string]string{"Authorization": "Bearer wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer = %d, want 401", rec.Code)
	}
}

func TestPasswordLoginAndCookieFlow(t *testing.T) {
	s := testServer(t, fakeWith("opencode"), "")
	h := s.Handler()

	rec := do(h, "POST", "/api/v1/auth/login", `{"password":"hunter2"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200", rec.Code)
	}
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	// Cookie works for reads without any origin headers.
	rec = do(h, "GET", "/api/v1/services", "", map[string]string{"Cookie": sessionCookie + "=" + cookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie read = %d, want 200", rec.Code)
	}
}

func TestWrongPasswordRejected(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()
	if rec := do(h, "POST", "/api/v1/auth/login", `{"password":"nope"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", rec.Code)
	}
}

func cookieHdr(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	rec := do(h, "POST", "/api/v1/auth/login", `{"password":"hunter2"}`, nil)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return map[string]string{"Cookie": sessionCookie + "=" + c.Value}
		}
	}
	t.Fatal("no session cookie from login")
	return nil
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestCSRFOnCookieMutations(t *testing.T) {
	f := fakeWith("opencode")
	h := testServer(t, f, "").Handler()
	ck := cookieHdr(t, h)

	// No Origin / Sec-Fetch-Site at all: not a browser page — reject.
	if rec := do(h, "POST", "/api/v1/services/opencode/bounce", "", ck); rec.Code != http.StatusForbidden {
		t.Fatalf("cookie mutation without origin headers = %d, want 403", rec.Code)
	}
	// Cross-origin: reject.
	if rec := do(h, "POST", "/api/v1/services/opencode/bounce", "", merge(ck, map[string]string{"Origin": "https://evil.example"})); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation = %d, want 403", rec.Code)
	}
	// Same-origin via Origin header (httptest requests have Host example.com).
	if rec := do(h, "POST", "/api/v1/services/opencode/bounce", "", merge(ck, map[string]string{"Origin": "https://example.com"})); rec.Code != http.StatusOK {
		t.Fatalf("same-origin mutation = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	// Same-origin via Sec-Fetch-Site.
	if rec := do(h, "POST", "/api/v1/services/opencode/bounce", "", merge(ck, map[string]string{"Sec-Fetch-Site": "same-origin"})); rec.Code != http.StatusOK {
		t.Fatalf("sec-fetch-site mutation = %d, want 200", rec.Code)
	}
	// Bearer-token mutations are exempt (no ambient credentials).
	if rec := do(h, "POST", "/api/v1/services/opencode/bounce", "", bearer); rec.Code != http.StatusOK {
		t.Fatalf("bearer mutation = %d, want 200", rec.Code)
	}
}

func TestVerbsExecuteAndReturnStatus(t *testing.T) {
	f := fakeWith("opencode")
	h := testServer(t, f, "").Handler()

	rec := do(h, "POST", "/api/v1/services/opencode/up", "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("up = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK     bool               `json:"ok"`
		Status keep.ServiceStatus `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Status.Name != "opencode" {
		t.Fatalf("unexpected verb response: %s", rec.Body.String())
	}
	if len(f.calls) != 1 || f.calls[0] != "up opencode" {
		t.Fatalf("orchestrator calls = %v", f.calls)
	}
}

func TestSelfDownBlockedButBounceAllowed(t *testing.T) {
	f := fakeWith("keep-web", "opencode")
	h := testServer(t, f, "keep-web").Handler()

	rec := do(h, "POST", "/api/v1/services/keep-web/down", "", bearer)
	if rec.Code != http.StatusConflict {
		t.Fatalf("self down = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "self_down_blocked") {
		t.Fatalf("self down body = %s", rec.Body.String())
	}
	if len(f.calls) != 0 {
		t.Fatalf("self down reached the orchestrator: %v", f.calls)
	}

	if rec := do(h, "POST", "/api/v1/services/keep-web/bounce", "", bearer); rec.Code != http.StatusOK {
		t.Fatalf("self bounce = %d, want 200", rec.Code)
	}
	// Downing anything else still works.
	if rec := do(h, "POST", "/api/v1/services/opencode/down", "", bearer); rec.Code != http.StatusOK {
		t.Fatalf("other down = %d, want 200", rec.Code)
	}
}

func TestUnknownServiceIs404(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()
	for _, probe := range [][2]string{
		{"GET", "/api/v1/services/ghost"},
		{"GET", "/api/v1/services/ghost/logs"},
		{"GET", "/api/v1/services/ghost/show"},
		{"POST", "/api/v1/services/ghost/up"},
	} {
		if rec := do(h, probe[0], probe[1], "", bearer); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", probe[0], probe[1], rec.Code)
		}
	}
}

func TestInvalidConfigIs503(t *testing.T) {
	s := testServer(t, nil, "")
	s.newOrch = func() (orchestrator, error) {
		return nil, configError{fmt.Errorf("yaml: line 3: mapping values are not allowed")}
	}
	h := s.Handler()
	rec := do(h, "GET", "/api/v1/services", "", bearer)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid config = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "config_invalid") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestLogsTail(t *testing.T) {
	f := fakeWith("opencode")
	f.logDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(f.logDir, "opencode.out.log"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := testServer(t, f, "").Handler()

	rec := do(h, "GET", "/api/v1/services/opencode/logs?lines=2", "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("logs = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := body["out"]; len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("out lines = %v, want [two three]", got)
	}
	if got := body["err"]; len(got) != 0 {
		t.Fatalf("err lines = %v, want empty (missing file)", got)
	}
}

func TestAuthStateAndMe(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()

	rec := do(h, "GET", "/api/v1/auth/state", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth/state = %d, want 200 (unauthenticated allowed)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"password_enabled": true`) {
		t.Fatalf("auth/state body = %s", rec.Body.String())
	}

	if rec := do(h, "GET", "/api/v1/auth/me", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me unauthenticated = %d, want 401", rec.Code)
	}
	rec = do(h, "GET", "/api/v1/auth/me", "", bearer)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"method": "token"`) {
		t.Fatalf("me with bearer = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()
	rec := do(h, "GET", "/api/v1/nope", "", bearer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api route = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unknown api route content-type = %s, want JSON", rec.Header().Get("Content-Type"))
	}
}

func TestNewRequiresSomeSecret(t *testing.T) {
	_, err := New(Options{StatePath: filepath.Join(t.TempDir(), "s.json")})
	if err == nil {
		t.Fatal("New with no password and no token should fail")
	}
}
