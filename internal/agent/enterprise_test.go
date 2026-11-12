package agent

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/edition"
)

func TestEnterpriseDesktopAgentRoster(t *testing.T) {
	t.Setenv("MAGPIE_EDITION", "")
	pluggedHome(t, aiderSrc)
	ids := func(agents []*Agent) []string {
		out := make([]string, len(agents))
		for i, a := range agents {
			out[i] = a.ID
		}
		return out
	}
	all, onHost := ids(All()), ids(local())
	if !slices.Contains(all, "claude-desktop") || !slices.Contains(all, "aider") {
		t.Fatalf("fixture is missing Claude Desktop or its installed agent plugin: %v", all)
	}

	t.Setenv("MAGPIE_EDITION", string(edition.Desktop))
	if got := ids(All()); !slices.Equal(got, all) {
		t.Errorf("enterprise desktop is missing agents: got %v, want %v", got, all)
	}
	if got := ids(local()); !slices.Equal(got, onHost) {
		t.Errorf("enterprise desktop local agent list: got %v, want %v", got, onHost)
	}
	if _, err := Find("claude-desktop"); err != nil {
		t.Error(err)
	}

	t.Setenv("MAGPIE_EDITION", string(edition.Server))
	if len(All()) != 0 || len(local()) != 0 {
		t.Error("enterprise server must not discover local agent configs")
	}
}
