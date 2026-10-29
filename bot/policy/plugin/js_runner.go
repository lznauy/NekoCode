package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"nekocode/bot/policy/contract"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const maxPluginReadFileBytes = 64 * 1024

func runPluginJS(pluginRoot string, event Event, action hookAction) (*Result, error) {
	event = contract.NormalizeEvent(event)
	root, err := os.OpenRoot(pluginRoot)
	if err != nil {
		return nil, fmt.Errorf("open plugin root: %w", err)
	}
	defer root.Close()
	program := action.program
	if program == nil {
		source, name, err := jsSource(root, action)
		if err != nil {
			return nil, err
		}
		program, err = goja.Compile(name, source, false)
		if err != nil {
			return nil, err
		}
	}

	timeout := action.Timeout
	if timeout <= 0 {
		timeout = 5000
	}

	ctx, cancel := context.WithTimeout(eventContext(event), time.Duration(timeout)*time.Millisecond)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vm := goja.New()
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()

	var logs []string
	jsEvent, err := eventObject(event)
	if err != nil {
		return nil, fmt.Errorf("encode hook event: %w", err)
	}
	jsEvent["pluginRoot"] = pluginRoot
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
		return readPluginFile(root, path)
	}); err != nil {
		return nil, fmt.Errorf("set readFile: %w", err)
	}

	value, err := vm.RunProgram(program)
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

func eventObject(event Event) (map[string]any, error) {
	// JSON-shaped values give JS and command handlers identical field names.
	event = contract.NormalizeEvent(event)
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(raw, &out)
	return out, err
}

func jsSource(root *os.Root, action hookAction) (source, name string, err error) {
	if len(action.Code) > maxPluginSourceBytes {
		return "", "", fmt.Errorf("JS source exceeds 1 MiB")
	}
	if action.Code != "" {
		return action.Code, "inline-plugin-hook.js", nil
	}
	if action.Path == "" {
		return "", "", fmt.Errorf("js hook requires code or path")
	}
	path, err := resolvePluginPath(action.Path)
	if err != nil {
		return "", "", err
	}
	data, err := readLimitedFile(root, path, maxPluginSourceBytes)
	if err != nil {
		return "", "", err
	}
	return string(data), filepath.Clean(action.Path), nil
}

func decodeJSResult(value goja.Value, schema []byte) (*Result, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	exported := value.Export()
	if s, ok := exported.(string); ok {
		if len(s) > maxPluginOutputBytes {
			return nil, fmt.Errorf("JS result exceeds %d bytes", maxPluginOutputBytes)
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		if err := validatePluginOutput(schema, s); err != nil {
			return nil, err
		}
		return &Result{Hint: &Hint{Type: "plugin_output", Severity: "info", Content: s}}, nil
	}

	raw, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("marshal js result: %w", err)
	}
	if err := validatePluginOutput(schema, string(raw)); err != nil {
		return nil, err
	}
	if len(raw) > maxPluginOutputBytes {
		return nil, fmt.Errorf("JS result exceeds %d bytes", maxPluginOutputBytes)
	}
	return decodeStructuredResult(raw)
}

func decodeStructuredResult(raw []byte) (*Result, error) {
	var fields map[string]json.RawMessage
	if err := strictJSON(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("hook result must be an object")
	}
	for _, pair := range [][2]string{{"blockTool", "block_tool"}, {"requireTool", "require_tool"}, {"blockFinal", "block_final"}, {"statePatch", "state_patch"}} {
		if value, ok := fields[pair[0]]; ok {
			if _, exists := fields[pair[1]]; exists {
				return nil, fmt.Errorf("duplicate result field %s", pair[1])
			}
			fields[pair[1]] = value
			delete(fields, pair[0])
		}
	}
	// Legacy JS hooks returned stop:{reason:...}; canonical stop is a decision.
	if stop, ok := fields["stop"]; ok && len(stop) > 0 && stop[0] == '{' {
		var legacy struct {
			Reason string `json:"reason"`
		}
		if err := strictJSON(stop, &legacy); err != nil {
			return nil, err
		}
		fields["stop"], _ = json.Marshal(legacy.Reason)
	}
	encoded, _ := json.Marshal(fields)
	var result Result
	if err := strictJSON(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode hook result: %w", err)
	}
	if _, err := contract.Control(result); err != nil {
		return nil, err
	}
	return &result, nil
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

func readPluginFile(root *os.Root, relPath string) (string, error) {
	path, err := resolvePluginPath(relPath)
	if err != nil {
		return "", err
	}
	data, err := readLimitedFile(root, path, maxPluginReadFileBytes)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

func resolvePluginPath(relPath string) (string, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("plugin file path must stay inside plugin root")
	}
	return clean, nil
}
