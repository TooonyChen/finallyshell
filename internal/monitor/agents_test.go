package monitor

import (
	"strings"
	"testing"
)

func TestFindAgent(t *testing.T) {
	// args is the command line, split at spaces.
	for _, tc := range []struct{ comm, args, want string }{
		// Each row, by its command and by its package.
		{"claude", "claude", "Claude Code"},
		{"node", "node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js --resume", "Claude Code"},
		{"codex", "/usr/local/lib/node_modules/@openai/codex/vendor/x86_64/codex/codex", "Codex"},
		{"node", "node /usr/bin/codex", "Codex"},
		{"node", "node /home/me/.npm-global/bin/gemini -y", "Gemini CLI"},
		{"node", "npx @google/gemini-cli", "Gemini CLI"},
		{"pi", "pi", "pi"},
		{"node", "node /usr/lib/node_modules/@mariozechner/pi-coding-agent/dist/cli.js", "pi"},
		{"opencode", "opencode", "opencode"},
		{"node", "bunx opencode-ai@latest", "opencode"},
		{"amp", "amp", "Amp"},
		{"node", "node /usr/lib/node_modules/@sourcegraph/amp/dist/main.js", "Amp"},
		{"cursor-agent", "/home/me/.local/bin/cursor-agent", "Cursor CLI"},
		{"copilot", "copilot", "GitHub Copilot CLI"},
		{"node", "node /usr/lib/node_modules/@github/copilot/index.js", "GitHub Copilot CLI"},
		{"qwen", "qwen", "Qwen Code"},
		{"node", "npx -y @qwen-code/qwen-code", "Qwen Code"},
		{"goose", "/usr/local/bin/goose session", "Goose"},
		{"kimi", "/home/me/.local/bin/kimi", "Kimi CLI"},
		{"kiro-cli", "kiro-cli chat", "Kiro CLI"},
		{"aider", "/usr/bin/python3 /home/me/.local/bin/aider --model sonnet", "Aider"},
		{"uvx", "uvx --from aider-chat aider", "Aider"},
		{"crush", "crush", "Crush"},
		{"droid", "droid", "Droid"},
		{"auggie", "auggie", "Auggie"},
		{"node", "node /usr/lib/node_modules/@augmentcode/auggie/augment.mjs", "Auggie"},

		// Through runtimes and launchers.
		{"node", "node --no-warnings --require /tmp/hook.js /usr/bin/claude", "Claude Code"},
		{"node", "node --title=x /opt/claude.js", "Claude Code"},
		{"bun", "bun /home/me/.bun/install/global/node_modules/@openai/codex/bin/codex.js", "Codex"},
		{"bun", "bun x @anthropic-ai/claude-code", "Claude Code"},
		{"deno", "deno run -A npm:@google/gemini-cli@0.9.0", "Gemini CLI"},
		{"python3.12", "python3.12 -m aider", "Aider"},
		{"python", "python -W ignore -m aider.main", "Aider"},
		{"python3", "python3 /usr/lib/python3/dist-packages/aider/__main__.py", "Aider"},
		{"npx", "npx --package=@openai/codex codex", "Codex"},
		{"npm exec @anthr", "npm exec @anthropic-ai/claude-code", "Claude Code"}, // its title, one argument
		{"pnpm", "pnpm dlx @sourcegraph/amp", "Amp"},
		{"node", "node /usr/lib/node_modules/pnpm/bin/pnpm.cjs dlx opencode-ai", ""}, // pnpm through node: not followed
		{"uvx", "uvx aider-chat==0.86.1", "Aider"},
		{"sudo", "sudo -u build claude", "Claude Code"},
		{"node", "node -- /usr/bin/qwen", "Qwen Code"},

		// Near misses.
		{"pip", "pip install foo", ""},
		{"claude-monitor", "/usr/bin/python3 /usr/local/bin/claude-monitor", ""},
		{"node", "node server.js", ""},
		{"node", "node -e require('claude')", ""},
		{"python3", "python3 -c import aider", ""},
		{"bash", "-bash", ""},
		{"vim", "vim claude.md", ""},
		{"node", "node /home/pi/server.js", ""},
		{"node", "node", ""},
		{"", "", ""},
		{"Claude", "Claude", ""},
	} {
		var args []string
		if tc.args != "" {
			args = strings.Split(tc.args, " ")
		}
		if tc.comm == "npm exec @anthr" {
			args = []string{tc.args, ""}
		}
		got := ""
		if a := FindAgent(tc.comm, args); a != nil {
			got = a.Name
		}
		if got != tc.want {
			t.Errorf("FindAgent(%q, %q) = %q, want %q", tc.comm, args, got, tc.want)
		}
	}
}

// Every row matches by each of its names, as the program's name or as
// the package node runs, and its names are its own.
func TestAgentsTable(t *testing.T) {
	seen := map[string]string{}
	for i := range Agents {
		a := &Agents[i]
		if a.Name == "" || len(a.Names) == 0 {
			t.Errorf("row %d: %+v", i, a)
		}
		for _, n := range a.Names {
			if other, ok := seen[n]; ok {
				t.Errorf("%q is %s and %s", n, other, a.Name)
			}
			seen[n] = a.Name
			if got := FindAgent(n, []string{n}); got != a {
				t.Errorf("%q is %v, want %s", n, got, a.Name)
			}
			if got := FindAgent("node", []string{"node", "/usr/lib/node_modules/" + n + "/cli.js"}); got != a {
				t.Errorf("node running %q is %v, want %s", n, got, a.Name)
			}
			if got := FindAgent("npx", []string{"npx", "-y", n + "@latest"}); got != a {
				t.Errorf("npx %q is %v, want %s", n, got, a.Name)
			}
		}
	}
}
