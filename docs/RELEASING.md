# Release Process

Only the release manager creates official NekoCode releases.
For the full pipeline, branch triggers, permissions, and failure handling, see
[CI_CD.md](CI_CD.md).

## Prepare

1. Start from a clean `master` branch with all required checks passing.
2. Move the `Unreleased` changelog entries into a versioned section. List
   breaking changes and required migrations explicitly.
3. Run the local verification commands from `docs/CONTRIBUTING.md` and test the TUI
   on at least one supported platform.
4. Confirm that installer changes work against a temporary installation
   directory.

## Publish

Create an annotated semantic version tag such as `v0.5.0` and push it. The
release workflow builds Linux and macOS binaries for amd64 and arm64, then
publishes:

- Platform binaries
- `SHA256SUMS`
- `sbom.spdx.json`
- GitHub build provenance attestations

The workflow generates release notes from merged pull requests. Review the
generated notes before announcing the release.

## Verify

1. Verify every binary and the SBOM listed in `SHA256SUMS`.
2. Run `scripts/install.sh --version <tag> --dir <temporary-directory>` on Linux
   or macOS.
3. Start `nekocode-tui` and confirm the displayed version matches the tag.
4. Keep the release or delete it immediately if any artifact or checksum is
   wrong. Never replace assets under an existing version tag.

Patch releases contain compatible fixes. Minor releases may add features and
must document migrations. Until v1.0, public Go APIs can change between minor
versions, but every breaking change must appear in the changelog.
