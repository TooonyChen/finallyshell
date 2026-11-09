package monitor

import (
	"path"
	"slices"
	"strings"
)

// Agent is a coding agent that can run in a session's shell, which the
// session's tab shows by its logo while it does.
type Agent struct {
	// Name is the agent's name as its makers write it, for the tooltip.
	Name string
	// Icon is the file of its logo in internal/icons/agents, without
	// ".svg": "" for an agent with none, which shows a generic one.
	Icon string
	// Names are the names its program runs by: its commands, and the
	// packages it is installed from, which a runtime such as node is
	// given. They match whole, so "pip" is not "pi".
	Names []string
}

// Agents are the coding agents a session tab knows. Adding one is a row
// here, its logo in internal/icons/agents and a case in the tests.
var Agents = []Agent{
	{Name: "Claude Code", Icon: "claudecode", Names: []string{"claude", "@anthropic-ai/claude-code"}},
	{Name: "Codex", Icon: "codex", Names: []string{"codex", "@openai/codex"}},
	{Name: "Gemini CLI", Icon: "geminicli", Names: []string{"gemini", "@google/gemini-cli"}},
	{Name: "pi", Icon: "pi", Names: []string{"pi", "@mariozechner/pi-coding-agent"}},
	{Name: "opencode", Icon: "opencode", Names: []string{"opencode", "opencode-ai"}},
	{Name: "Amp", Icon: "amp", Names: []string{"amp", "@sourcegraph/amp"}},
	{Name: "Cursor CLI", Icon: "cursor", Names: []string{"cursor-agent"}},
	{Name: "GitHub Copilot CLI", Icon: "githubcopilot", Names: []string{"copilot", "@github/copilot"}},
	{Name: "Qwen Code", Icon: "qwen", Names: []string{"qwen", "@qwen-code/qwen-code"}},
	{Name: "Goose", Icon: "goose", Names: []string{"goose"}},
	{Name: "Kimi CLI", Icon: "kimi", Names: []string{"kimi"}},
	{Name: "Kiro CLI", Icon: "kiro", Names: []string{"kiro-cli"}},
	{Name: "Aider", Names: []string{"aider", "aider-chat"}},
	{Name: "Crush", Names: []string{"crush"}},
	{Name: "Droid", Names: []string{"droid"}},
	{Name: "Auggie", Names: []string{"auggie", "@augmentcode/auggie"}},
}

// agentsByName finds an agent by any of its names.
var agentsByName = func() map[string]*Agent {
	m := map[string]*Agent{}
	for i := range Agents {
		for _, n := range Agents[i].Names {
			m[n] = &Agents[i]
		}
	}
	return m
}()

// FindAgent tells the coding agent that a program is, by the name the
// kernel has for it (comm) and its arguments: the agent one of the two,
// or the base name of its first argument, is a name of; or, when that
// argument is a runtime or launcher, such as node or npx, the agent of
// the script or package it runs. It is nil for any other program.
func FindAgent(comm string, args []string) *Agent {
	// A program that set its title, as node's process.title does, may
	// have all of it in its first argument, spaces and all.
	if len(args) > 0 && strings.Contains(args[0], " ") && strings.Join(args[1:], "") == "" {
		args = strings.Fields(args[0])
	}
	names := []string{comm}
	if len(args) > 0 {
		prog := path.Base(args[0])
		names = append(names, prog)
		if l, ok := launcherOf(prog); ok {
			names = append(names, l.target(args[1:])...)
		}
	}
	for _, n := range names {
		if a := agentsByName[n]; a != nil {
			return a
		}
	}
	return nil
}

// launcher is a program that runs another one, as node a script or npx a
// package, by how its arguments name that one.
type launcher struct {
	// value are the flags whose value is the next argument, unless
	// written --flag=value.
	value []string
	// pkg are the flags whose value is the program run: npx -p, uvx
	// --from, python -m.
	pkg []string
	// stop are the flags after which no program to run comes, as node -e
	// runs code it is given.
	stop []string
}

var (
	nodeLauncher = launcher{
		value: []string{"-r", "--require", "--import", "--loader", "--experimental-loader", "-C", "--conditions", "--title", "--env-file"},
		stop:  []string{"-e", "--eval", "-p", "--print"},
	}
	pythonLauncher = launcher{
		value: []string{"-W", "-X"},
		pkg:   []string{"-m"},
		stop:  []string{"-c"},
	}
	npmLauncher = launcher{
		value: []string{"-w", "--workspace", "-C", "--dir", "--prefix", "--filter", "-F"},
		pkg:   []string{"-p", "--package"},
		stop:  []string{"-c", "--call"},
	}
	uvLauncher = launcher{
		value: []string{"--with", "-w", "--with-requirements", "-p", "--python", "--index", "--index-url", "--extra-index-url", "--spec"},
		pkg:   []string{"--from"},
	}
	// launchers are the launchers by the base name of their program;
	// python3.12 and the like are pythonLauncher too.
	launchers = map[string]launcher{
		"node": nodeLauncher, "nodejs": nodeLauncher,
		"bun":  {value: []string{"--cwd", "-r", "--preload", "--config"}, stop: []string{"-e", "--eval", "-p", "--print"}},
		"deno": {value: []string{"--config", "-c", "--import-map", "-L", "--log-level", "--location", "--cert"}},
		"npx":  npmLauncher, "bunx": npmLauncher, "pnpm": npmLauncher, "pnpx": npmLauncher, "npm": npmLauncher, "yarn": npmLauncher,
		"uvx": uvLauncher, "uv": uvLauncher, "pipx": uvLauncher,
		"sudo": {value: []string{"-u", "-g", "-h", "-p", "-C", "-D", "-r", "-t", "-U", "-T", "-R"}},
		"doas": {value: []string{"-u", "-C"}},
	}
	// launcherWords are the subcommands of a launcher that come before
	// the program it runs: npm exec, pnpm dlx, deno run, uv tool run.
	launcherWords = map[string]bool{"exec": true, "x": true, "dlx": true, "run": true, "tool": true}
)

// launcherOf is the launcher prog is, if it is one.
func launcherOf(prog string) (launcher, bool) {
	if l, ok := launchers[prog]; ok {
		return l, true
	}
	if v, ok := strings.CutPrefix(prog, "python"); ok && strings.Trim(v, "0123456789.") == "" {
		return pythonLauncher, true
	}
	return launcher{}, false
}

// target is the names of the program a launcher's arguments run, as
// names candidates gives them; nil when they run none.
func (l launcher) target(args []string) []string {
	words := true // before the program: its subcommands
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = false
			continue
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 && words {
			flag, v, hasValue := strings.Cut(a, "=")
			switch {
			case slices.Contains(l.stop, flag):
				return nil
			case slices.Contains(l.pkg, flag):
				if !hasValue {
					if i++; i >= len(args) {
						return nil
					}
					v = args[i]
				}
				names := candidates(v)
				if pkg, _, ok := strings.Cut(v, "."); ok && flag == "-m" {
					names = append(names, pkg) // python -m aider.main
				}
				return names
			case slices.Contains(l.value, flag) && !hasValue:
				i++
			}
			continue
		}
		if words && launcherWords[a] {
			continue
		}
		return candidates(a)
	}
	return nil
}

// candidates are the names a launcher's argument can match an agent by:
// the package or module it names, without its version or registry
// ("npm:@openai/codex@latest", "aider-chat==0.86"); its base name, also
// without a script's extension ("/usr/bin/gemini", "cli.js"); and the
// package installed at the path, after node_modules or site-packages
// (".../node_modules/@anthropic-ai/claude-code/cli.js").
func candidates(arg string) []string {
	a := arg
	for _, p := range []string{"npm:", "jsr:"} {
		a = strings.TrimPrefix(a, p)
	}
	// The version, after the @ that doesn't start a scope, or a Python
	// requirement's.
	if at := strings.LastIndex(a, "@"); at > 0 && !strings.Contains(a[at:], "/") {
		a = a[:at]
	}
	if i := strings.IndexAny(a, "=<>~![; "); i >= 0 && !strings.Contains(a, "/") {
		a = a[:i]
	}
	base := path.Base(a)
	names := []string{a, base}
	for _, ext := range []string{".js", ".mjs", ".cjs", ".ts", ".py"} {
		if n, ok := strings.CutSuffix(base, ext); ok {
			names = append(names, n)
		}
	}
	segs := strings.Split(a, "/")
	for i := len(segs) - 2; i >= 0; i-- {
		if s := segs[i]; s != "node_modules" && s != "site-packages" && s != "dist-packages" {
			continue
		}
		pkg := segs[i+1]
		if strings.HasPrefix(pkg, "@") && i+2 < len(segs) {
			pkg += "/" + segs[i+2]
		}
		names = append(names, pkg)
		break
	}
	return names
}
