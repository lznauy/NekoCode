#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest_dir="$repo_root/flatpak"
manifest="org.nekocode.NekoCode.yml"
manifest_path="$manifest_dir/$manifest"
work_dir="$repo_root/.build/flatpak"
source_dir="$work_dir/source"
cache_dir="$work_dir/cache"
state_dir="$work_dir/state"
generated_manifest="$work_dir/org.nekocode.NekoCode.generated.yml"
build_dir="$work_dir/app"
bundle="$repo_root/nekocode.flatpak"
app_id="org.nekocode.NekoCode"
runtime_id="org.gnome.Platform"
sdk_id="org.gnome.Sdk"
runtime_version="$(sed -n 's/^runtime-version: *"\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' "$manifest_path" | head -1)"
npm_registry="${NPM_REGISTRY:-https://registry.npmjs.org}"
install_deps=1
clean=0
clean_cache=0
clean_all=0
prepare_only=0

usage() {
  cat <<'EOF'
Usage: bash flatpak/build.sh [--bundle] [--clean] [--clean-cache] [--clean-all] [--no-install] [--prepare-only]

  --bundle      Export nekocode.flatpak after installing the app locally.
  --clean       Reset transient Flatpak build state before building.
  --clean-cache Reset Go/npm caches used by the Flatpak build.
  --clean-all   Delete the whole .build/flatpak directory before building.
  --no-install  Do not auto-install missing Flatpak runtime or SDK.
  --prepare-only
                Generate the source snapshot and local manifest, then exit.

Environment:
  NPM_REGISTRY  npm registry used inside the Flatpak build sandbox.
                Default: https://registry.npmjs.org
EOF
}

quote_shell_arg() {
  printf '%q' "$1"
}

maybe_reexec_in_nix_shell() {
  if [[ "$prepare_only" == "1" || -n "${NEKOCODE_FLATPAK_NIX_SHELL:-}" || ! -f "$repo_root/shell.nix" ]]; then
    return
  fi

  local missing=0
  for cmd in flatpak flatpak-builder appstreamcli; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      missing=1
    fi
  done

  if [[ "$missing" != "1" ]]; then
    return
  fi

  if ! command -v nix-shell >/dev/null 2>&1; then
    return
  fi

  local quoted_args=()
  local arg
  for arg in "$@"; do
    quoted_args+=("$(quote_shell_arg "$arg")")
  done

  echo "Re-entering nix-shell shell.nix for Flatpak packaging tools..."
  exec nix-shell "$repo_root/shell.nix" --run "cd $(quote_shell_arg "$repo_root") && NEKOCODE_FLATPAK_NIX_SHELL=1 bash flatpak/build.sh ${quoted_args[*]}"
}

make_bundle=0
for arg in "$@"; do
  case "$arg" in
    --bundle) make_bundle=1 ;;
    --clean) clean=1 ;;
    --clean-cache) clean_cache=1 ;;
    --clean-all) clean_all=1 ;;
    --no-install) install_deps=0 ;;
    --prepare-only) prepare_only=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

maybe_reexec_in_nix_shell "$@"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing command: $1" >&2
    if [[ "$1" == "flatpak-builder" ]]; then
      echo "Install flatpak-builder first. On NixOS, add pkgs.flatpak-builder or enter nix-shell shell.nix." >&2
    fi
    exit 1
  fi
}

ensure_remote() {
  if flatpak --user remotes --columns=name | grep -Fxq flathub; then
    return
  fi
  if [[ "$install_deps" != "1" ]]; then
    echo "Missing user Flatpak remote: flathub" >&2
    exit 1
  fi
  flatpak --user remote-add --if-not-exists flathub https://flathub.org/repo/flathub.flatpakrepo
}

is_installed() {
  flatpak --user info "$1//$2" >/dev/null 2>&1
}

install_ref() {
  local ref="$1"
  local branch="$2"
  if is_installed "$ref" "$branch"; then
    return
  fi
  if [[ "$install_deps" != "1" ]]; then
    echo "Missing user Flatpak ref: $ref//$branch" >&2
    exit 1
  fi
  echo "Installing Flatpak ref: $ref//$branch"
  flatpak --user install --noninteractive --or-update --no-related -y flathub "$ref//$branch"
}

require_cmd flatpak
require_cmd flatpak-builder
require_cmd rsync

if [[ -z "$runtime_version" ]]; then
  echo "Could not read runtime-version from $manifest_path" >&2
  exit 1
fi

if [[ "$clean_all" == "1" ]]; then
  rm -rf "$work_dir"
elif [[ "$clean_cache" == "1" ]]; then
  rm -rf "$cache_dir"
fi

ensure_appstreamcli() {
  if command -v appstreamcli >/dev/null 2>&1; then
    return
  fi

  echo "Missing command: appstreamcli" >&2
  echo "On NixOS, enter nix-shell shell.nix or install an AppStream package that provides appstreamcli." >&2
  exit 1
}

ensure_user_export_permissions() {
  local hicolor_index="$HOME/.local/share/flatpak/exports/share/icons/hicolor/index.theme"
  if [[ -e "$hicolor_index" && ! -w "$hicolor_index" ]]; then
    echo "Fixing user Flatpak icon index permissions: $hicolor_index"
    chmod u+w "$hicolor_index"
  fi
}

if command -v desktop-file-validate >/dev/null 2>&1; then
  desktop-file-validate "$repo_root/flatpak/org.nekocode.NekoCode.desktop"
fi

if command -v xmllint >/dev/null 2>&1; then
  xmllint --noout "$repo_root/flatpak/org.nekocode.NekoCode.metainfo.xml"
fi

mkdir -p "$work_dir" "$cache_dir"
rm -rf "$source_dir"
mkdir -p "$source_dir"

rsync -a --delete \
  --exclude='/.git/' \
  --exclude='/.flatpak-builder/' \
  --exclude='/.build/' \
  --exclude='/build-flatpak/' \
  --exclude='/node_modules/' \
  --exclude='/gui/node_modules/' \
  --exclude='/gui/dist/' \
  --exclude='/build/' \
  --exclude='/result' \
  --exclude='/result-dev' \
  --exclude='/nekocode.flatpak' \
  --exclude='/flatpak/.work/' \
  --exclude='/flatpak/.flatpak-builder/' \
  "$repo_root/" "$source_dir/"

sed \
  -e "s|@FLATPAK_SOURCE_DIR@|$source_dir|g" \
  -e "s|@FLATPAK_CACHE_DIR@|$cache_dir|g" \
  -e "s|@NPM_REGISTRY@|$npm_registry|g" \
  "$manifest_path" > "$generated_manifest"

if [[ "$prepare_only" == "1" ]]; then
  echo "Prepared $source_dir"
  echo "Generated $generated_manifest"
  exit 0
fi

ensure_remote
install_ref "$runtime_id" "$runtime_version"
install_ref "$sdk_id" "$runtime_version"
ensure_appstreamcli
ensure_user_export_permissions

if [[ -d "$state_dir/build" ]]; then
  chmod -R u+rwX "$state_dir/build" || true
fi
if [[ -d "$state_dir/rofiles" ]]; then
  chmod -R u+rwX "$state_dir/rofiles" || true
  rm -rf "$state_dir/rofiles" || true
fi
chmod -R u+rwX "$cache_dir" || true

(
  cd "$manifest_dir"
  builder_args=(--force-clean --disable-rofiles-fuse --user --install)
  if [[ "$clean" == "1" ]]; then
    rm -rf "$state_dir/build" "$state_dir/rofiles" "$cache_dir/go-build" "$cache_dir/npm/_cacache/tmp"
  fi
  build_log="$work_dir/flatpak-builder.log"
  if flatpak-builder --state-dir="$state_dir" "${builder_args[@]}" "$build_dir" "$generated_manifest" 2>&1 | tee "$build_log"; then
    exit 0
  fi
  if grep -Fq "Update is older than current version" "$build_log"; then
    echo "Existing user install is newer than this local build; reinstalling $app_id"
    flatpak --user uninstall -y "$app_id"
    flatpak-builder --state-dir="$state_dir" "${builder_args[@]}" "$build_dir" "$generated_manifest"
    exit 0
  fi
  exit 1
)

if [[ "$make_bundle" == "1" ]]; then
  flatpak build-bundle "$HOME/.local/share/flatpak/repo" "$bundle" "$app_id"
  echo "Wrote $bundle"
fi
