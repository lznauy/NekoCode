# Governance

NekoCode currently uses a maintainer-led governance model. The repository owner
is the release manager and final reviewer for changes to security boundaries,
public APIs, persistence formats, and release automation.

## Decisions

Small fixes and documentation changes can proceed through pull requests. Open
an issue before work that changes a public API, adds a dependency, changes a
security boundary, or introduces a new roadmap item. Record accepted design
decisions in the issue or the relevant architecture document so later changes
have a durable rationale.

The maintainer may request more tests, a migration path, or a narrower change
before merging. Lack of response does not imply approval.

## Reviews

Every pull request requires a passing CI run and maintainer review. Authors do
not merge their own pull requests without another qualified reviewer when a
second maintainer is available. Security-sensitive changes require an explicit
threat-model review.

`CODEOWNERS` records the current review owner. Ownership can be delegated by a
pull request that updates this document and `CODEOWNERS` together.

## Releases

The release manager selects a tested commit, updates the changelog, creates a
semantic version tag, and verifies the generated artifacts. Released changelog
entries are historical records and are not rewritten except to correct a clear
factual error. See [RELEASING.md](RELEASING.md) for the release
checklist.

## Conduct and security

Community participation follows [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
Report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.
