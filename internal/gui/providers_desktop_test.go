package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edition"
	"github.com/yetone/magpie/internal/provider"
)

func TestEnterpriseDesktopProviders(t *testing.T) {
	sandboxHome(t)
	forgetLastProviders()
	t.Cleanup(forgetLastProviders)

	t.Setenv("MAGPIE_EDITION", string(edition.Server))
	if err := provider.Save(provider.Provider{ID: "legacy", Name: "Legacy", Chat: "http://127.0.0.1:1/v1", Off: true}); err != nil {
		t.Fatal(err)
	}
	keepProviders(providersState())

	t.Setenv("MAGPIE_EDITION", string(edition.Desktop))
	if err := provider.Save(provider.Provider{ID: "office", Name: "Office", Preset: provider.RemoteMagpiePreset,
		Chat: "http://127.0.0.1:1", Key: "employee-test-key", Off: true}); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	request := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
		return w
	}
	if w := request(http.MethodGet, "/api/providers?last=1"); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatalf("desktop used another edition's cached providers: %d %s", w.Code, w.Body)
	}
	w := request(http.MethodGet, "/api/providers")
	var state providersJSON
	if err := json.Unmarshal(w.Body.Bytes(), &state); w.Code != http.StatusOK || err != nil {
		t.Fatalf("providers: %d %s", w.Code, w.Body)
	}
	if len(state.Presets) != 1 || state.Presets[0].ID != provider.RemoteMagpiePreset ||
		len(state.Providers) != 1 || state.Providers[0].ID != "office" {
		t.Fatalf("desktop providers: %+v, presets: %+v", state.Providers, state.Presets)
	}
	if len(state.Movable)+len(state.Unmoved)+len(state.Excluded)+len(state.OnPlugins) != 0 {
		t.Fatalf("desktop exposed subscription metadata: %+v", state)
	}
	if w := request(http.MethodPost, "/api/signin"); w.Code != http.StatusNotFound {
		t.Fatalf("desktop sign-in route returned %d", w.Code)
	}
}
