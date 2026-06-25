#!/usr/bin/env bash
# NekoCode GUI 开发环境初始化
# 在 nix-shell 内执行: source gui/env.sh

PROJ_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="$PROJ_DIR/build"

# 1. Go bin to PATH
export PATH="$PATH:$(go env GOPATH)/bin"

# 2. 编译 sigwrap
mkdir -p "$BUILD_DIR"
if [ ! -f "$BUILD_DIR/libsigwrap.so" ]; then
    gcc -shared -fPIC -o "$BUILD_DIR/libsigwrap.so" "$PROJ_DIR/gui/sigwrap.c" -ldl
    echo "sigwrap: compiled $BUILD_DIR/libsigwrap.so"
fi

# 3. LD_PRELOAD
export LD_PRELOAD="$BUILD_DIR/libsigwrap.so${LD_PRELOAD:+:$LD_PRELOAD}"

# 4. WebKitGTK Wayland 渲染兜底
# WebKitGTK 2.46+ 在 Wayland 原生合成器 + 分数缩放路径下 LayoutUnit 换算异常,
# 导致 1px 边框被舍入为 0.5px、整窗边距/ring/圆角等可视化塌缩(但 CSS 本身正确).
# 强制走 XWayland 即可绕开该渲染分支.
# 仅在 Wayland 会话下生效, 不影响 X11 / 无显示等场景.
if [ -z "$GDK_BACKEND" ] && [ -n "$WAYLAND_DISPLAY" ]; then
    export GDK_BACKEND="x11"
    echo "env: GDK_BACKEND=x11 (WebKitGTK Wayland fractional-scaling workaround)"
fi

echo "env: LD_PRELOAD=$LD_PRELOAD"