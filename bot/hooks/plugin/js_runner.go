package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const maxPluginReadFileBytes = 64 * 1024

type pluginSchemaError struct{ err error }

func (e pluginSchemaError) Error() string { return e.err.Error() }

func runPluginJS(pluginRoot string, event Event, action hookAction) (*Result, error) {
	source, name, err := jsSource(pluginRoot, action)
	if err != nil {
		return nil, err
	}

	timeout := action.Timeout
	if timeout <= 0 {
		timeout = 5000
	}

	vm := goja.New()
	timer := time.AfterFunc(time.Duration(timeout)*time.Millisecond, func() {
		vm.Interrupt("plugin JS hook timed out")
	})
	defer timer.Stop()

	var logs []string
	jsEvent := eventObject(event)
	if err := vm.Set("event", jsEvent); err != nil {
		return nil, fmt.Errorf("set event: %w", err)
	}
	if err := vm.Set("context", jsEvent); err != nil {
		return nil, fmt.Errorf("set context: %w", err)
	}
	if err := vm.Set("console", map[string]func(...goja.Value){
		"log":   func(args ...goja.Value) { appendConsoleLog(&logs, "log", args...) },
		"warn":  func(args ...goja.Value) { appendConsoleLog(&logs, "warn", args...) },
		"error": func(args ...goja.Value) { appendConsoleLog(&logs, "error", args...) },
	}); err != nil {
		return nil, fmt.Errorf("set console: %w", err)
	}
	if err := vm.Set("readFile", func(path string) (string, error) {
		return readPluginFile(pluginRoot, path)
	}); err != nil {
		return nil, fmt.Errorf("set readFile: %w", err)
	}

	value, err := vm.RunScript(name, source)
	if err != nil {
		return nil, err
	}

	if hook := vm.Get("hook"); hook != nil && !goja.IsUndefined(hook) && !goja.IsNull(hook) {
		fn, ok := goja.AssertFunction(hook)
		if !ok {
			return nil, fmt.Errorf("global hook is not a function")
		}
		value, err = fn(goja.Undefined(), vm.ToValue(jsEvent))
		if err != nil {
			return nil, err
		}
	}

	result, err := decodeJSResult(value, action.OutputSchema)
	if err != nil {
		return nil, err
	}
	if result == nil && len(logs) > 0 {
		return &Result{Hint: &Hint{
			Type:     "plugin_console",
			Severity: "info",
			Content:  strings.Join(logs, "\n"),
		}}, nil
	}
	return result, nil
}

func eventObject(event Event) map[string]any {
	return map[string]any{
		"tool":       event.Tool,
		"error":      event.Error,
		"pluginRoot": "",
		"state": map[string]any{
			"ints":    map[string]int{},
			"strings": map[string]string{},
		},
	}
}

func jsSource(pluginRoot string, action hookAction) (source, name string, err error) {
	if action.Code != "" {
		return action.Code, "inline-plugin-hook.js", nil
	}
	if action.Path == "" {
		return "", "", fmt.Errorf("js hook requires code or path")
	}
	path, err := resolvePluginPath(pluginRoot, action.Path)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read js hook: %w", err)
	}
	return string(data), filepath.Clean(action.Path), nil
}

func decodeJSResult(value goja.Value, schema []byte) (*Result, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	exported := value.Export()
	if s, ok := exported.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		if err := validatePluginOutput(schema, s); err != nil {
			return nil, pluginSchemaError{err: err}
		}
		return &Result{Hint: &Hint{Type: "plugin_output", Severity: "info", Content: s}}, nil
	}

	raw, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("marshal js result: %w", err)
	}
	if err := validatePluginOutput(schema, string(raw)); err != nil {
		return nil, pluginSchemaError{err: err}
	}
	return decodeStructuredResult(raw)
}

func decodeStructuredResult(raw []byte) (*Result, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("js result must be an object: %w", err)
	}
	if len(m) == 0 {
		return nil, nil
	}

	var out Result
	if err := decodeField(m, "hint", "hint", &out.Hint); err != nil {
		return nil, err
	}
	if err := decodeField(m, "stop", "stop", &out.Stop); err != nil {
		return nil, err
	}
	if err := decodeField(m, "block_tool", "blockTool", &out.BlockTool); err != nil {
		return nil, err
	}
	if err := decodeField(m, "require_tool", "requireTool", &out.RequireTool); err != nil {
		return nil, err
	}
	if err := decodeField(m, "block_final", "blockFinal", &out.BlockFinal); err != nil {
		return nil, err
	}
	if err := decodeField(m, "state_patch", "statePatch", &out.StatePatch); err != nil {
		return nil, err
	}
	return &out, nil
}

func decodeField[T any](m map[string]json.RawMessage, snake, camel string, dst **T) error {
	raw, ok := m[snake]
	if !ok {
		raw, ok = m[camel]
	}
	if !ok || string(raw) == "null" {
		return nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("decode %s: %w", snake, err)
	}
	*dst = &v
	return nil
}

func jsActionName(action hookAction) string {
	if action.Path != "" {
		return action.Path
	}
	return "inline js"
}

func appendConsoleLog(logs *[]string, level string, args ...goja.Value) {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, fmt.Sprint(arg.Export()))
	}
	line := fmt.Sprintf("[%s] %s", level, strings.Join(parts, " "))
	current := 0
	for _, l := range *logs {
		current += len(l) + 1
	}
	if current+len(line) > maxPluginOutputBytes {
		return
	}
	*logs = append(*logs, line)
}

func readPluginFile(pluginRoot, relPath string) (string, error) {
	path, err := resolvePluginPath(pluginRoot, relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("readFile requires a file path")
	}
	if info.Size() > maxPluginReadFileBytes {
		return "", fmt.Errorf("file exceeds %d byte read limit", maxPluginReadFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

func resolvePluginPath(pluginRoot, relPath string) (string, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("plugin file path must stay inside plugin root")
	}
	root, err := filepath.EvalSymlinks(pluginRoot)
	if err != nil {
		return "", fmt.Errorf("resolve plugin root: %w", err)
	}
	target := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve plugin file: %w", err)
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("check plugin file path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("plugin file path must stay inside plugin root")
	}
	return resolved, nil
}
