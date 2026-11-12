package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/edition"
	"github.com/yetone/magpie/internal/sessions"
	"github.com/yetone/magpie/internal/usage"
)

func TestEnterpriseUsageSessions(t *testing.T) {
	sandboxHome(t)
	t.Setenv("MAGPIE_EDITION", string(edition.Server))
	forgetOwnSessions()
	t.Cleanup(forgetOwnSessions)
	usage.Append(usage.Record{
		Time: time.Now(), Agent: "claude", Session: "usage-session",
		Provider: "relay", Model: "model", Input: 42, Output: 8, Status: http.StatusOK,
	})

	h := Handler(nil, nil)
	get := func(path string, out any) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	const key = "claude:usage-session"
	var list sessionsJSON
	get("/api/sessions", &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != "usage-session" || !list.Sessions[0].Gateway ||
		list.Sessions[0].Input != 42 || list.Terminal || len(list.Dirs) != 0 {
		t.Fatalf("session list: %+v", list)
	}
	var stats sessions.Stats
	get("/api/sessions/stats?days=7", &stats)
	if !slices.Equal(stats.Keys, []string{key}) {
		t.Fatalf("stats session keys: %v", stats.Keys)
	}
	var overview sessions.Overview
	get("/api/sessions/overview?days=7&agent=claude", &overview)
	if overview.Count != 1 {
		t.Fatalf("overview session count: %d", overview.Count)
	}
	var detail sessionJSON
	get("/api/sessions/one?key="+key, &detail)
	if detail.ID != "usage-session" || detail.Input != 42 || detail.Resume != "" || detail.Path != "" {
		t.Fatalf("session detail: %+v", detail)
	}
	get("/api/sessions/progress", &map[string]any{})
	for _, path := range []string{"/api/sessions/manage", "/api/sessions/transcript", "/api/sessions/markdown"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, w.Code)
		}
	}
}
