# NekoCode GUI 开发环境
# Shell 进入后:
#   nix-shell shell.nix
#   source env.sh
#   wails dev

{ pkgs ? import <nixpkgs> {} }:

let
  webkitgtk4 = pkgs.webkitgtk_4_1;
in
pkgs.mkShell {
  name = "nekocode-gui-dev";

  buildInputs = [
    pkgs.go
    pkgs.gtk3
    pkgs.gtk3.dev
    webkitgtk4
    webkitgtk4.dev
    pkgs.libsoup_3
    pkgs.libsoup_3.dev
    pkgs.pkg-config
    pkgs.nodejs_22
    pkgs.gcc
    pkgs.flatpak
    pkgs.flatpak-builder
    pkgs.desktop-file-utils
    pkgs.libxml2
    pkgs.appstream
  ];

  # 额外 pkg-config 路径
  NIX_PKG_CONFIG_EXTRA = "${pkgs.libsoup_3.dev}/lib/pkgconfig";

  # Wails 构建标签
  CGO_LDFLAGS = "-Wl,-rpath,${webkitgtk4}/lib";

  shellHook = ''
    export PATH="$PATH:$(go env GOPATH)/bin"
    export PKG_CONFIG_PATH="$PKG_CONFIG_PATH''${PKG_CONFIG_PATH:+:}$NIX_PKG_CONFIG_EXTRA"

    echo "== NekoCode GUI dev shell =="
    echo "Go:    $(go version)"
    echo "Node:  $(node --version)"
    echo "Flatpak builder: $(flatpak-builder --version 2>/dev/null || echo 'FAIL')"
    echo "GTK3:  $(pkg-config --modversion gtk+-3.0 2>/dev/null || echo 'FAIL')"
    echo "WK2GTK: $(pkg-config --modversion webkit2gtk-4.1 2>/dev/null || echo 'FAIL')"
    echo "libsoup: $(pkg-config --modversion libsoup-3.0 2>/dev/null || echo 'FAIL')"
    echo "pango:  $(pkg-config --modversion pango 2>/dev/null || echo 'FAIL')"
    echo ""
    echo ">> source gui/env.sh && wails dev"
  '';
}
