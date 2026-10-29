package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ExitError carries the process status for the one-shot command.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode maps command errors to stable shell statuses.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var status *ExitError
	if errors.As(err, &status) {
		return status.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 130
	}
	return 1
}

type onceSchema struct {
	text     string
	resolved *jsonschema.Schema
}

func prepareOnce(ctx context.Context, options Options) (*onceSchema, error) {
	if options.OutputSchema == "" {
		return nil, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	file, err := openSchemaFile(options.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("output schema must be a regular file")
	}
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stop()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	if len(data) > 1<<20 {
		return nil, errors.New("output schema exceeds 1 MiB")
	}

	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(localSchemaOnly{})
	compiler.AssertFormat()
	const resource = "https://nekocode.invalid/output-schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	resolved, err := compiler.Compile(resource)
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}

	return &onceSchema{text: string(data), resolved: resolved}, nil
}

// Only the supplied document and built-in meta-schemas may be resolved.
type localSchemaOnly struct{}

func (localSchemaOnly) Load(string) (any, error) {
	return nil, errors.New("external schema references are unsupported; use local #/$defs references")
}

// onceCapture receives complete frames from the existing transport writer.
// It retains only the final result; previews and tool output stay off stdout.
type onceCapture struct{ result map[string]any }

func (w *onceCapture) Write(data []byte) (int, error) {
	var frame map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&frame); err != nil {
		return 0, err
	}
	if frame["type"] == "result" {
		w.result = frame
	}
	return len(data), nil
}
func (*onceCapture) Close() error { return nil }

func serveOnce(ctx context.Context, in io.ReadCloser, out io.WriteCloser, backend Backend, cwd string, options Options) error {
	schema := options.schema
	if schema == nil {
		var err error
		schema, err = prepareOnce(ctx, options)
		if err != nil {
			return &ExitError{Code: inputExitCode(err), Err: err}
		}
	}
	prompt := options.Prompt
	if prompt == "" {
		stop := context.AfterFunc(ctx, func() { _ = in.Close() })
		data, err := io.ReadAll(io.LimitReader(in, maxFrameSize+1))
		stop()
		if ctx.Err() != nil {
			return &ExitError{Code: 130, Err: ctx.Err()}
		}
		if err != nil {
			return &ExitError{Code: 2, Err: fmt.Errorf("read prompt: %w", err)}
		}
		if len(data) > maxFrameSize {
			return &ExitError{Code: 2, Err: errors.New("text input exceeds 8 MiB")}
		}
		prompt = string(data)
	}
	if strings.TrimSpace(prompt) == "" {
		return &ExitError{Code: 2, Err: errors.New("empty prompt")}
	}
	if len(prompt) > maxFrameSize {
		return &ExitError{Code: 2, Err: errors.New("text input exceeds 8 MiB")}
	}
	if schema != nil {
		prompt += "\n\nReturn your final answer as exactly one JSON value matching the following JSON Schema. Do not use Markdown fences or include commentary outside the JSON.\n" + schema.text
	}
	if len(prompt) > maxFrameSize {
		return &ExitError{Code: 2, Err: errors.New("prompt and output schema together exceed 8 MiB")}
	}
	options.Prompt = prompt
	options.once = true
	capture := &onceCapture{}
	err := Serve(ctx, io.NopCloser(strings.NewReader("")), capture, backend, cwd, options)
	result := capture.result
	if result == nil {
		result = map[string]any{"type": "result", "result": "", "session_id": backend.CurrentSessionID()}
	}
	if err == nil && result["is_error"] == true {
		if result["subtype"] == "error_cancelled" {
			err = context.Canceled
		} else {
			err = fmt.Errorf("run failed (%v): %v", result["subtype"], result["errors"])
		}
	}
	if err == nil && capture.result == nil {
		err = errors.New("run ended without a final result")
	}
	outputValidationFailed := false
	if err == nil && schema != nil {
		text, _ := result["result"].(string)
		var value any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if parseErr := decoder.Decode(&value); parseErr != nil {
			err = fmt.Errorf("final output is not JSON: %w", parseErr)
			outputValidationFailed = true
		} else if decoder.Decode(new(any)) != io.EOF {
			err = errors.New("final output must contain exactly one JSON value")
			outputValidationFailed = true
		} else if validateErr := schema.resolved.Validate(value); validateErr != nil {
			err = fmt.Errorf("final output does not match schema: %w", validateErr)
			outputValidationFailed = true
		} else {
			result["structured_output"] = json.RawMessage(text)
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		result["is_error"] = true
		result["errors"] = []string{err.Error()}
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), result["subtype"] == "error_cancelled":
			result["subtype"] = "error_cancelled"
		case outputValidationFailed:
			result["subtype"] = "error_output_validation"
		case result["subtype"] == nil, result["subtype"] == "success":
			result["subtype"] = "error_during_execution"
		}
	} else {
		result["is_error"] = false
	}
	// Duplicate stdio handles can be closed on cancellation to unblock pipe
	// backpressure. The process-wide originals remain owned by the CLI.
	stop := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer stop()
	if options.OutputFormat == "json" {
		if writeErr := json.NewEncoder(out).Encode(result); writeErr != nil {
			err = errors.Join(err, writeErr)
		}
	} else if err == nil {
		text, _ := result["result"].(string)
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		n, writeErr := io.WriteString(out, text)
		if writeErr == nil && n != len(text) {
			writeErr = io.ErrShortWrite
		}
		if writeErr != nil {
			err = writeErr
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return &ExitError{Code: ExitCode(err), Err: err}
	}
	return nil
}

func inputExitCode(err error) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 130
	}
	return 2
}
