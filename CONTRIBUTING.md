# Contributing

Thanks for helping. Issues and pull requests are welcome.

## License of contributions

Finallyshell is offered under the PolyForm Noncommercial License 1.0.0 or
the PolyForm Internal Use License 1.0.0 (see [LICENSE](LICENSE)), and its
author also offers it under separate commercial licenses. To keep that
possible, contributions are accepted on these terms:

By submitting a contribution (code, documentation, translations, images
or anything else) to this repository, you:

1. grant Tony Chen (the licensor named in LICENSE) and anyone who receives
   the software from him a perpetual, worldwide, non-exclusive, royalty-free,
   irrevocable license to use, copy, change, distribute, sublicense and
   relicense your contribution under any terms, including commercial ones;
2. grant the same people a license under any patents you can license that
   your contribution, alone or combined with the software, would infringe;
3. confirm that the contribution is your own work, or that you have the
   right to submit it on these terms (for example, your employer agrees),
   and say so in the pull request if part of it comes from someone else
   or is under another license.

You keep the copyright in your contribution. If you can't agree to these
terms, open an issue instead of a pull request, and describe the change.

## How we work

- Every change starts as a GitHub issue that says how to solve it; every
  pull request links its issue (`Closes #N`).
- Branches use Conventional Branch (`feature/...`, `bugfix/...`,
  `chore/...`), commits and pull request titles use Conventional Commits.
- `go test -race ./...`, `go vet ./...` and `go tool mygo vet` must pass.

[CLAUDE.md](CLAUDE.md) has the full rules, and
[docs/en/development.md](docs/en/development.md) how to build, run and test the
app.
