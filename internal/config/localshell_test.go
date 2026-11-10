package config

import (
	"errors"
	"slices"
	"testing"
)

func TestSplitCommandLine(t *testing.T) {
	for _, tc := range []struct {
		line    string
		windows bool
		want    []string
	}{
		{"", false, nil},
		{"   \t ", false, nil},
		{"wsl.exe", true, []string{"wsl.exe"}},
		{"pwsh -NoLogo", true, []string{"pwsh", "-NoLogo"}},
		{"  /opt/homebrew/bin/fish   -l ", false, []string{"/opt/homebrew/bin/fish", "-l"}},
		{`sh -c 'echo ready; cat'`, false, []string{"sh", "-c", "echo ready; cat"}},
		{`sh -c "echo \"hi\" \$HOME \n"`, false, []string{"sh", "-c", `echo "hi" $HOME \n`}},
		{`a ""  b ''`, false, []string{"a", "", "b", ""}},
		{`my\ shell -x`, false, []string{"my shell", "-x"}},
		{`it's"  "fine`, false, nil}, // unclosed: checked below
		{`ab'c d'e`, false, []string{"abc de"}},
		{`trailing\`, false, []string{`trailing\`}},
		{`C:\Windows\System32\cmd.exe /k`, true, []string{`C:\Windows\System32\cmd.exe`, "/k"}},
		{`"C:\Program Files\PowerShell\7\pwsh.exe" -NoLogo`, true, []string{`C:\Program Files\PowerShell\7\pwsh.exe`, "-NoLogo"}},
		{`\\server\share\sh.exe`, true, []string{`\\server\share\sh.exe`}},
		{"中文 参数", false, []string{"中文", "参数"}},
	} {
		got, err := SplitCommandLine(tc.line, tc.windows)
		if tc.line == `it's"  "fine` {
			if !errors.Is(err, ErrUnclosedQuote) {
				t.Errorf("%q: %q, %v; want an unclosed quote", tc.line, got, err)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tc.want) || (got == nil) != (tc.want == nil) {
			t.Errorf("SplitCommandLine(%q, %v) = %q, %v; want %q", tc.line, tc.windows, got, err, tc.want)
		}
	}
	for _, line := range []string{`sh -c 'echo`, `"pwsh`, `a "b c`} {
		if _, err := SplitCommandLine(line, false); !errors.Is(err, ErrUnclosedQuote) {
			t.Errorf("%q: %v, want an unclosed quote", line, err)
		}
	}
}

// localShell stays on this device: it is not in what sync carries.
func TestLocalShellNotSynced(t *testing.T) {
	s := Settings{LocalShell: "/opt/homebrew/bin/fish -l", TermTheme: "Dracula"}
	m := SyncedSettings(s)
	if _, ok := m["localShell"]; ok {
		t.Errorf("localShell synced: %v", m)
	}
	if _, ok := m["termTheme"]; !ok {
		t.Errorf("termTheme not synced: %v", m)
	}
}
