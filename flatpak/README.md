# Flatpak packaging

This directory contains the distributable Flatpak build. It builds Go, npm, and
Wails inside the Flatpak SDK/runtime, so the resulting app does not depend on
`/nix/store`.

## Prerequisites

- `flatpak`
- `flatpak-builder`

The build helper uses the user Flatpak repository. It checks for Flathub and the
GNOME runtime/SDK, and installs missing refs automatically. It writes all
Flatpak build intermediates under `.build/flatpak`, including the clean source
snapshot, generated manifest, Flatpak builder state, and Go/npm caches.

Runtime/SDK installation uses `--no-related`, so Flatpak locale extensions such
as `org.gnome.Platform.Locale` are not pulled automatically during packaging.
They are not required to compile NekoCode.

If Flatpak refuses a local reinstall with "Update is older than current
version", the helper removes the existing user-level `org.nekocode.NekoCode`
install and retries once. It does not remove shared runtimes or SDKs.

On NixOS, if `flatpak-builder` or `appstreamcli` is missing from the current
shell, `build.sh` re-enters `shell.nix` automatically so AppStream metadata is
composed by the host Nix package rather than by a nested Flatpak SDK process.

Go and Node are downloaded as official tarballs inside the Flatpak build
sandbox, so there are no freedesktop SDK extension branch prompts.
It also disables `rofiles-fuse` to avoid stale FUSE mount cleanup failures on
NixOS.

The publish manifest currently targets GNOME runtime/SDK `50`, Go `1.26.3`,
and Node `22.11.0`. Go and Node tarballs are declared as Flatpak sources with
SHA-256 checksums, so flatpak-builder can cache them between builds.

```bash
bash flatpak/build.sh
```

The script uses `https://registry.npmjs.org` for npm by default. If your network
works better with a mirror, override it explicitly:

```bash
NPM_REGISTRY=https://registry.npmmirror.com bash flatpak/build.sh
```

Reset transient Flatpak builder state when needed:

```bash
bash flatpak/build.sh --clean
```

Reset dependency caches when a Go/npm cache gets stuck or corrupted:

```bash
bash flatpak/build.sh --clean-cache
```

Remove every Flatpak build intermediate and start from scratch:

```bash
bash flatpak/build.sh --clean-all
```

To disable automatic runtime/SDK installation:

```bash
bash flatpak/build.sh --no-install
```

## Distributable Build

From the repository root:

```bash
bash flatpak/build.sh
flatpak run org.nekocode.NekoCode
```

Export a local bundle:

```bash
bash flatpak/build.sh --bundle
```

Install the bundle on another machine:

```bash
flatpak install ./nekocode.flatpak
flatpak run org.nekocode.NekoCode
```

This build currently uses network access during `flatpak-builder` so Go modules,
the Wails CLI, and npm packages can be fetched inside the Flatpak SDK. For
Flathub-style fully offline builds, vendor the Go modules, npm dependencies, and
Wails CLI source explicitly in the manifest.

## Notes

- `org.nekocode.NekoCode.png` is a labeled placeholder icon. Replace it with a
  real project icon before publishing.
- The manifest grants `--filesystem=home` and network access because NekoCode is
  a coding assistant. This does not automatically expose the host toolchain
  inside the Flatpak sandbox. Host command execution may require a separate
  `flatpak-spawn --host` integration or helper process.
