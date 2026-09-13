# Contributing

NekoCode is a single-binary, embeddable AI coding assistant core with a strong
focus on prompt-cache cost. Read [the architecture guide](ARCHITECTURE.md)
before starting a large change. Open an issue before implementing an
undecided feature.

## Development environment

- Go 1.25.12, as declared in `go.mod`
- Zig for CI-compatible CGO builds
- Node.js 20 or later for `interaction/gui/web` and `official`

Run these checks before opening a pull request:

```bash
go build ./...
go test ./...
go vet ./...
go test -race ./bot/... ./runtime/...
govulncheck ./...

cd interaction/gui/web
npm ci
npm test
npm run build
```

## Code and tests

- Keep dependencies flowing from `bot` to `runtime` to `interaction`.
- Limit each change to the requested behavior. Avoid unrelated renames and
  refactors.
- Preserve real error semantics. Do not turn an unknown or failed state into a
  successful empty value.
- Add focused tests for new behavior and bug fixes. Test observable behavior,
  boundaries, and invariants rather than implementation text.
- Run `gofmt` on Go code and keep `go vet` clean.
- Describe the threat model, trust boundaries, and failure modes for changes to
  permissions, command execution, file access, credentials, or logs.

## Commits and pull requests

Keep commits independently reviewable and reversible. Reference an issue or
task when one exists. Update `docs/CHANGELOG.md` for user-visible changes,
and do not rewrite entries for released versions.

Use the pull request template and report the exact commands or manual paths you
tested. CI checks Go formatting, builds, tests, races, reachable vulnerabilities,
the GUI frontend, the project website, shell scripts, and documentation links.

## Documentation

Update the relevant document with behavior changes:

- `docs/ARCHITECTURE.md` for module responsibilities and data flow
- `docs/USER_GUIDE.md` for user-facing behavior

Most detailed developer documents are currently written in Chinese. English
translations can be contributed independently as long as they retain the same
technical meaning.

## Community

By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). Read
[GOVERNANCE.md](GOVERNANCE.md) for maintainer and release responsibilities and
[SUPPORT.md](SUPPORT.md) for support boundaries. Report vulnerabilities through
the private channels in [SECURITY.md](SECURITY.md).
