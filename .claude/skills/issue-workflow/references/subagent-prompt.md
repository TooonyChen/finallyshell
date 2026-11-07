# Subagent prompts

Read this when dispatching subagents to work on issues in parallel.

Launch each one with the Agent tool, `isolation: "worktree"` (its own git
worktree, so parallel agents don't share a checkout) and
`run_in_background: true`, all in one message so they run at the same
time. A subagent starts with none of this conversation's context: the
prompt is everything it knows, so it must carry the issue, the state of
main and the rules. When it finishes, its report comes back to you; to
send review feedback or a rebase request, `SendMessage` to it, and it
resumes with its context intact.

## What a good prompt says

1. What the project is, and where the docs of its libraries are (MyGo's
   module directory), and "read CLAUDE.md and README.md first".
2. What's already on main that it builds on, with names: functions,
   types, files and the PRs that added them. It can't know what other
   agents merged an hour ago.
3. The issue: title, body and acceptance criteria, copied in full, plus
   any decisions the owner made since the issue was written.
4. Design points to decide and explain, where you have an opinion, and
   the traps you already know of (a function that must stay the only
   path, a rule like "no plain Ctrl+letter shortcuts on Windows/Linux").
5. Who else is working in parallel, on what files, and what this agent
   owns. Ask it to put new code in new files and keep shared files'
   edits small and additive. Name the single hooks shared work should go
   through, so that two agents extend one function instead of writing two.
6. The "how to work" block below.
7. What to report back.

## How to work (paste and fill in)

```text
## How to work
- Isolated git worktree. Start: `git fetch origin main && git checkout -b <type>/<short-name> origin/main`.
- Conventional Commits, English, PR title like `<type>(<scope>): <description>`. End every commit message with the session's attribution trailer lines (copy them in). No model names anywhere else.
- Before pushing: `gofmt -l .` empty; `go build ./...`, `go vet ./...` (also `GOOS=windows go vet .` and `GOOS=darwin go vet .`), `go tool mygo vet`, `go test -race ./...` all pass. New UI text in both locales/en.json and locales/zh-Hans.json, then `go tool mygo generate`, commit locales/catalog_gen.go. Goroutines change app state only through App.post.
- Live tests: if a test sshd runs (e.g. 127.0.0.1:2222, user/password given here), also run `FINALLYSHELL_TEST_SSH=user:password@127.0.0.1:2222 go test -race ./...`. Don't stop or reconfigure it; other agents share it.
- Screenshots in English and Simplified Chinese to <scratchpad>/shots/<issue>/ (don't commit); look at them with Read and fix layout; list them in the PR.
- Update README when behavior or layout changes.
- Push `git push -u origin <branch>` (retry with backoff on network errors only). Open the PR with the GitHub MCP tool (ToolSearch "select:mcp__github__create_pull_request"), base main. Body per .github/pull_request_template.md: `## Summary` with "Closes #<issue>" (or "Refs #<issue>" if it doesn't resolve all of it), `## Testing` including what was not tested. Plain writing. End with the session's PR attribution footer (copy it in).
- Don't merge, comment on issues, or push other branches. I review and squash-merge; I may message you for fixes or rebases (then fix, rerun everything, push with --force-with-lease).

## Report back
PR URL/number, design decisions, files changed, tests, check results, screenshot paths, open questions.
```

## Messages after dispatch

- After any merge that touches an agent's area, send it a heads-up: what
  merged, which functions or files changed, and what to plug into ("use
  `terminalOptions`, don't add a parallel helper"). No reply needed until
  its normal report.
- Review feedback goes in one message: each change, why, and "rebase onto
  origin/main, rerun all checks, push with --force-with-lease, reply with
  the new head SHA".
- Answer its open questions in the same message, so it doesn't stall.
