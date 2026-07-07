package serve

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
)

// fakeWithUpdate builds a fakeOrch whose services declare update commands.
func fakeWithUpdate(names ...string) *fakeOrch {
	f := &fakeOrch{}
	for _, n := range names {
		f.services = append(f.services, config.Service{Name: n, Update: []string{"/bin/echo updated"}})
	}
	return f
}

func TestUpdateStreamsAndCompletes(t *testing.T) {
	f := fakeWithUpdate("opencode")
	h := testServer(t, f, "").Handler()

	rec := do(h, "POST", "/api/v1/services/opencode/update", "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want SSE", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data: {"line":"updating..."}`) {
		t.Fatalf("stream missing output line event:\n%s", body)
	}
	if !strings.Contains(body, `"done":true`) || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("stream missing terminal result event:\n%s", body)
	}
	if len(f.calls) != 1 || f.calls[0] != "update opencode" {
		t.Fatalf("orchestrator calls = %v", f.calls)
	}
}

func TestUpdateFailureReportedInResult(t *testing.T) {
	f := fakeWithUpdate("opencode")
	f.updateErr = fmt.Errorf("update[0]: exit status 1")
	h := testServer(t, f, "").Handler()

	rec := do(h, "POST", "/api/v1/services/opencode/update", "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d (SSE always commits 200): %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "exit status 1") {
		t.Fatalf("stream missing failure result:\n%s", body)
	}
}

func TestUpdateSelfBlocked(t *testing.T) {
	f := fakeWithUpdate("keep-web")
	h := testServer(t, f, "keep-web").Handler()

	rec := do(h, "POST", "/api/v1/services/keep-web/update", "", bearer)
	if rec.Code != http.StatusConflict {
		t.Fatalf("self update = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "self_update_blocked") {
		t.Fatalf("self update body = %s", rec.Body.String())
	}
	if len(f.calls) != 0 {
		t.Fatalf("self update reached the orchestrator: %v", f.calls)
	}
}

func TestUpdateWithoutCommandsRejected(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()

	rec := do(h, "POST", "/api/v1/services/opencode/update", "", bearer)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update without commands = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no_update_commands") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestUpdateInProgressRejected(t *testing.T) {
	f := fakeWithUpdate("opencode")
	f.updating = true
	h := testServer(t, f, "").Handler()

	rec := do(h, "POST", "/api/v1/services/opencode/update", "", bearer)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-progress update = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "update_in_progress") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestUpdateUnknownServiceIs404(t *testing.T) {
	h := testServer(t, fakeWith("opencode"), "").Handler()
	if rec := do(h, "POST", "/api/v1/services/ghost/update", "", bearer); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown update = %d, want 404", rec.Code)
	}
}

func TestUpdateRequiresAuth(t *testing.T) {
	h := testServer(t, fakeWithUpdate("opencode"), "").Handler()
	if rec := do(h, "POST", "/api/v1/services/opencode/update", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated update = %d, want 401", rec.Code)
	}
}
