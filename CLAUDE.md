# Finalshelly

An SSH client in Go laid out like FinalShell, with vertical session tabs.
UI: MyGo native UI (`github.com/egoist/mygo/ui`); terminal: MyGo's terminal
plugin (libghostty-vt). See README.md for the package layout.

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

### Pull requests

- The title is a Conventional Commit line, as it becomes the squash commit.
- The body follows `.github/pull_request_template.md`: what changed and why,
  and how it was tested.
- PRs are squash-merged into `main`.

## Working on the code

- `go test -race ./...` must pass. UI code is tested with `ui.NewTester`
  (headless; `tt.Image()` gives a frame for screenshots, `SHOT_DIR=dir`
  makes the tests save theirs).
- `TestSSH` in `internal/app` runs against a real server when
  `FINALSHELLY_TEST_SSH=user:password@host:port` is set.
- New UI text goes in `locales/en.json` and `locales/zh-Hans.json`, then
  `go tool mygo generate`; commit the regenerated `locales/catalog_gen.go`.
- Run `go vet ./...` and `go tool mygo vet` before committing.
- Goroutines change app state only through `App.post`.
