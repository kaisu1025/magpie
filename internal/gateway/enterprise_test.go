package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/provider"
)

func TestEnterpriseServerRequiresAnEnabledKeyOnLoopback(t *testing.T) {
	fresh(t)
	t.Setenv("MAGPIE_EDITION", "enterprise-server")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("caller key reached the upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":5}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "plan", Key: "upstream-secret", Chat: up.URL + "/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Employee")
	s := httptest.NewServer(New().Front())
	defer s.Close()
	call := func(key string) int {
		t.Helper()
		r, err := http.NewRequest(http.MethodPost, s.URL+"/v1/chat/completions", strings.NewReader(chatReq))
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := s.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if got := call(""); got != http.StatusUnauthorized {
		t.Fatalf("an unkeyed request to the enterprise listener got %d, want 401", got)
	}
	if got := call(secrets[0]); got != http.StatusOK {
		t.Fatalf("an enabled key got %d, want 200", got)
	}
	if rec := lastUsage(t); rec.CallerKeyID != keys[0].ID {
		t.Fatalf("call usage belongs to %q, want %q", rec.CallerKeyID, keys[0].ID)
	}
	if _, err := access.Update("remove-key", access.Change{Key: keys[0].ID}); err != nil {
		t.Fatal(err)
	}
	if got := call(secrets[0]); got != http.StatusUnauthorized {
		t.Fatalf("a removed key got %d, want 401", got)
	}
}
