# 构建说明

构建命令需要在仓库根目录执行，入口文件是 `build/makefile`：

```bash
make -f build/makefile help
```

## 环境要求

- Go 1.26.10 或更高版本
- GNU Make
- Zig，构建 daemon 或静态发布产物时需要
- Node.js 和 npm，只有构建 GUI 时需要

## 本机编译

同时构建本机 TUI 和静态 Linux daemon：

```bash
make -f build/makefile build
```

产物会写到仓库根目录：

```text
nekocode
nekocode-daemon
```

也可以单独构建。`daemon` 会根据当前 Go 架构生成 amd64 或 arm64 的静态 Linux 二进制：

```bash
make -f build/makefile tui
make -f build/makefile daemon
```

安装 TUI 到 Go 的 bin 目录：

```bash
make -f build/makefile install
```

## 静态交叉编译

静态构建使用 Zig 编译 CGO 依赖，并链接 musl。一次构建 Linux amd64 和 arm64：

```bash
make -f build/makefile static
```

只构建一个架构：

```bash
make -f build/makefile static-linux-amd64
make -f build/makefile static-linux-arm64
```

产物位于 `dist/`：

```text
dist/nekocode-tui-linux-amd64
dist/nekocode-daemon-linux-amd64
dist/nekocode-tui-linux-arm64
dist/nekocode-daemon-linux-arm64
```

如果 Zig 不在默认的 `PATH` 中，可以指定可执行文件：

```bash
make -f build/makefile static ZIG=/path/to/zig
```

## 测试和检查

```bash
make -f build/makefile test
make -f build/makefile vet
```

构建 GUI：

```bash
make -f build/makefile gui
```

删除 Makefile 生成的二进制文件：

```bash
make -f build/makefile clean
```

## 自定义构建信息

Makefile 默认使用当前 Git 版本写入二进制，也可以手动指定版本号或产物目录：

```bash
make -f build/makefile build VERSION=v0.5.0
make -f build/makefile static DIST_DIR=release
```
