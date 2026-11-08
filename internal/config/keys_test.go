package config

import "testing"

// A host's Backspace and Delete are its own choice, else the settings',
// else ^? and ESC [ 3 ~; values this version does not know count as
// unset.
func TestHostKeySequences(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings Settings
		host     Host
		bs       BackspaceKey
		del      DeleteKey
	}{
		{"defaults", Settings{}, Host{}, BackspaceDEL, DeleteVT220},
		{"from the settings", Settings{TermBackspace: BackspaceCtrlH, TermDelete: DeleteDEL}, Host{}, BackspaceCtrlH, DeleteDEL},
		{"the host's own", Settings{TermBackspace: BackspaceCtrlH, TermDelete: DeleteCtrlH},
			Host{Backspace: BackspaceDEL, Delete: DeleteVT220}, BackspaceDEL, DeleteVT220},
		{"host ^H over the default", Settings{}, Host{Backspace: BackspaceCtrlH, Delete: DeleteCtrlH}, BackspaceCtrlH, DeleteCtrlH},
		{"unknown values", Settings{TermBackspace: "vt220", TermDelete: "bogus"}, Host{Backspace: "x", Delete: "y"}, BackspaceDEL, DeleteVT220},
		{"unknown host value follows the settings", Settings{TermBackspace: BackspaceCtrlH}, Host{Backspace: "x"}, BackspaceCtrlH, DeleteVT220},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.settings.HostBackspace(&tc.host); got != tc.bs {
				t.Errorf("backspace %q, want %q", got, tc.bs)
			}
			if got := tc.settings.HostDelete(&tc.host); got != tc.del {
				t.Errorf("delete %q, want %q", got, tc.del)
			}
		})
	}
}
