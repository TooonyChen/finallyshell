# Finallyshell

An SSH client in Go laid out like FinalShell, with vertical session tabs.
UI: MyGo native UI (`github.com/egoist/mygo/ui`); terminal: MyGo's terminal
plugin (libghostty-vt). See docs/en/development.md for the package layout.

## Repository rules

Use English for branch names, commit messages, PR titles and PR bodies.
Code comments and user-facing strings in `locales/` follow their own
language as usual.

### Branches: Conventional Branch

`<type>/<short-description>`, lowercase, words joined by hyphens:

- `feature/` new features, `bugfix/` fixes, `hotfix/` urgent fixes,
  `release/` release preparation, `chore/` everything else (docs, deps,
  CI, refactors, tests).
- Examples: `feature/sftp-rename`, `bugfix/ctrl-c-forwarding`,
  `chore/test-split-dividers`.
- Never commit to `main` directly; open a PR.

### Commits: Conventional Commits 1.0

`<type>(<optional scope>): <description>`, imperative, lowercase start, no
trailing period, at most about 72 characters. Types: `feat`, `fix`,
`docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`,
`revert`. Breaking changes use `!` after the type or a `BREAKING CHANGE:`
footer. Scopes are package or area names: `app`, `sshx`, `remotefs`,
`monitor`, `config`, `widgets`, `locales`, `terminal`, `settings`.

### Issues first

Work is issue-driven; the `issue-workflow` skill
(`.claude/skills/issue-workflow/`) has the whole loop. The rules:

- Before code, research the problem's possible solutions and write the
  chosen one, with the options and acceptance criteria, into a GitHub
  issue. Ask the owner only about product decisions, with a
  recommendation.
- Every PR has an issue. No issue yet, even for a small follow-up: open
  one first.
- One issue, one branch, one PR.

### Pull requests

- The title is a Conventional Commit line, as it becomes the squash commit.
- The body follows `.github/pull_request_template.md`: what changed and why,
  and how it was tested.
- The body links its issue: `Closes #N` when the PR resolves it, so the
  merge closes the issue; `Refs #N` when it resolves only part of it, or
  follows up a closed one.
- PRs are squash-merged into `main`, never merge commits or rebase merges.
  The squash commit is the PR title plus ` (#PR)`, and its message keeps
  the `Closes #N` line.

## Working on the code

- `go test -race ./...` must pass. UI code is tested with `ui.NewTester`
  (headless; `tt.Image()` gives a frame for screenshots, `SHOT_DIR=dir`
  makes the tests save theirs).
- `TestSSH` in `internal/app` runs against a real server when
  `FINALLYSHELL_TEST_SSH=user:password@host:port` is set.
- New UI text goes in `locales/en.json` and `locales/zh-Hans.json`, then
  `go tool mygo generate`; commit the regenerated `locales/catalog_gen.go`.
- Run `go vet ./...` and `go tool mygo vet` before committing.
- Goroutines change app state only through `App.post`.
- Docs: behavior goes in `features.md`, files and settings keys in
  `configuration.md`, building and the code in `development.md`, each in
  `docs/en/` and `docs/zh-CN/`: change both languages together, keeping
  the same headings in the same order. `README.md` and `README.zh-CN.md`
  show the headline features and must say the same thing; when a scene
  they show changes, run `go run ./tools/screenshots` and commit
  `docs/screenshots/`.
