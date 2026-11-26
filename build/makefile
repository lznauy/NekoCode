# NekoCode build entrypoint.
# Run targets from the repository root:
#   make -f build/makefile help

SHELL := /bin/sh

GO       ?= go
ZIG      ?= zig
DIST_DIR ?= dist
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DAEMON_GOARCH ?= $(shell $(GO) env GOARCH)

TUI_BIN    := nekocode
DAEMON_BIN := nekocode-daemon
GUI_BIN    := nekocode-gui

TUI_SRC    := ./cmd/tui
DAEMON_SRC := ./cmd/daemon
GUI_SRC    := .
GUI_TAGS   := desktop,production,webkit2_41
GUI_DIR    := interaction/gui/web

LDFLAGS        := -s -w -X nekocode/util/version.Version=$(VERSION)
STATIC_LDFLAGS := $(LDFLAGS) -linkmode external -extldflags=-static
BUILD_FLAGS    := -trimpath
STATIC_TAGS    := netgo,osusergo
DAEMON_ZIG_TARGET := $(if $(filter amd64,$(DAEMON_GOARCH)),x86_64-linux-musl,$(if $(filter arm64,$(DAEMON_GOARCH)),aarch64-linux-musl,))

.DEFAULT_GOAL := build

.PHONY: help build tui daemon install test vet gui \
	static static-linux-amd64 static-linux-arm64 check-zig check-daemon-arch clean

help:
	@echo "NekoCode build targets"
	@echo "  build                Build host TUI and daemon"
	@echo "  tui                   Build host TUI ($(TUI_BIN))"
	@echo "  daemon                Build static Linux daemon for the current Go architecture"
	@echo "  static                Cross-compile static Linux amd64 and arm64 binaries"
	@echo "  static-linux-amd64    Cross-compile static Linux amd64 binaries with Zig"
	@echo "  static-linux-arm64    Cross-compile static Linux arm64 binaries with Zig"
	@echo "  test                  Run the Go test suite"
	@echo "  vet                   Run go vet"
	@echo "  gui                    Build the web frontend and GUI"
	@echo "  clean                  Remove generated binaries"

build: tui daemon

tui:
	$(GO) build $(BUILD_FLAGS) -ldflags='$(LDFLAGS)' -o $(TUI_BIN) $(TUI_SRC)

daemon: check-zig check-daemon-arch
	GOOS=linux GOARCH=$(DAEMON_GOARCH) CGO_ENABLED=1 \
		CC="$(ZIG) cc -target $(DAEMON_ZIG_TARGET)" \
		CXX="$(ZIG) c++ -target $(DAEMON_ZIG_TARGET)" \
		$(GO) build $(BUILD_FLAGS) -tags='$(STATIC_TAGS)' -ldflags='$(STATIC_LDFLAGS)' \
		-o $(DAEMON_BIN) $(DAEMON_SRC)

install:
	$(GO) install $(BUILD_FLAGS) -ldflags='$(LDFLAGS)' $(TUI_SRC)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./cmd/tui ./cmd/daemon ./bot/... ./runtime/... ./protocol/... ./interaction/... ./logger/... ./util/...

# GUI requires Node/npm and Wails-compatible host dependencies.
gui:
	cd $(GUI_DIR) && npm run build
	$(GO) build $(BUILD_FLAGS) -tags='$(GUI_TAGS)' -ldflags='$(LDFLAGS)' -o $(GUI_BIN) $(GUI_SRC)

check-zig:
	@command -v $(ZIG) >/dev/null 2>&1 || { \
		echo "error: Zig is required for static cross-compilation (https://ziglang.org/download/)" >&2; \
		exit 1; \
	}

check-daemon-arch:
	@if [ -z "$(DAEMON_ZIG_TARGET)" ]; then \
		echo "error: daemon static build supports amd64 and arm64, got $(DAEMON_GOARCH)" >&2; \
		exit 1; \
	fi

static: static-linux-amd64 static-linux-arm64

static-linux-amd64: check-zig
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
		CC="$(ZIG) cc -target x86_64-linux-musl" \
		CXX="$(ZIG) c++ -target x86_64-linux-musl" \
		$(GO) build $(BUILD_FLAGS) -tags='$(STATIC_TAGS)' -ldflags='$(STATIC_LDFLAGS)' \
		-o $(DIST_DIR)/nekocode-tui-linux-amd64 $(TUI_SRC)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
		CC="$(ZIG) cc -target x86_64-linux-musl" \
		CXX="$(ZIG) c++ -target x86_64-linux-musl" \
		$(GO) build $(BUILD_FLAGS) -tags='$(STATIC_TAGS)' -ldflags='$(STATIC_LDFLAGS)' \
		-o $(DIST_DIR)/nekocode-daemon-linux-amd64 $(DAEMON_SRC)

static-linux-arm64: check-zig
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
		CC="$(ZIG) cc -target aarch64-linux-musl" \
		CXX="$(ZIG) c++ -target aarch64-linux-musl" \
		$(GO) build $(BUILD_FLAGS) -tags='$(STATIC_TAGS)' -ldflags='$(STATIC_LDFLAGS)' \
		-o $(DIST_DIR)/nekocode-tui-linux-arm64 $(TUI_SRC)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
		CC="$(ZIG) cc -target aarch64-linux-musl" \
		CXX="$(ZIG) c++ -target aarch64-linux-musl" \
		$(GO) build $(BUILD_FLAGS) -tags='$(STATIC_TAGS)' -ldflags='$(STATIC_LDFLAGS)' \
		-o $(DIST_DIR)/nekocode-daemon-linux-arm64 $(DAEMON_SRC)

clean:
	rm -f $(TUI_BIN) $(DAEMON_BIN) $(GUI_BIN)
	rm -f $(DIST_DIR)/nekocode-tui-linux-amd64 \
		$(DIST_DIR)/nekocode-daemon-linux-amd64 \
		$(DIST_DIR)/nekocode-tui-linux-arm64 \
		$(DIST_DIR)/nekocode-daemon-linux-arm64
	@rmdir $(DIST_DIR) 2>/dev/null || true
