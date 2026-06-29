BIN     := nekocode
TUI_SRC := ./cmd/main.go
GUI_SRC := ./main.go
GUI_DIR := gui

.PHONY: build run install clean test gui

build:
	go build -o $(BIN) $(TUI_SRC)

run: build
	./$(BIN)

install:
	go install $(TUI_SRC)

clean:
	rm -f $(BIN)

test:
	go test ./... 2>&1 | grep -vE '^ok|^?' --line-buffered || true

# GUI 需要先构建前端: cd $(GUI_DIR) && npm run build
gui:
	cd $(GUI_DIR) && npm run build
	go build -o nekocode-gui $(GUI_SRC)
