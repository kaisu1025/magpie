package gui

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/agentenv"
)

// folderOnly is the app's windows as far as an export needs them.
type folderOnly struct{ Windows }

func (folderOnly) OpenFolder(string) error { return nil }

// sandboxHome gives the test a machine of its own: no agents, no magpie files.
func sandboxHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", "")
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	t.Setenv("APPDATA", "")
	t.Setenv("LOCALAPPDATA", "")
	return h
}

type pickFolder struct {
	folderOnly
	dir string
}

func (p pickFolder) ChooseFolder(string) (string, error) { return p.dir, nil }

// Settings' Back up by itself (jwaitq on Discord): a change sends only what
// it changes, so the rest stays as the user set it; Change… takes a folder
// from the picker; a backup made now is listed, and restoring it says which
// backup of what was here undoes it.
