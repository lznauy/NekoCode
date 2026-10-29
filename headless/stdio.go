package headless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"nekocode/runtime/standard"
)

// ParseArgs recognizes the headless mode without consuming TUI/ACP arguments.
func ParseArgs(args []string) (Options, bool, error) {
	options := Options{InputFormat: "text"}
	selected := len(args) > 0 && args[0] == "exec"
	execMode := selected
	if execMode {
		args = args[1:]
	}
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")
		switch key {
		case "--headless", "--stream-json", "-p", "--print", "--input-format", "--output-format", "--output-schema":
			selected = true
		}
	}
	if !selected {
		return options, false, nil
	}
	output, streamMode, printMode, promptSet, partial := "", false, execMode, false, false
	for i := 0; i < len(args); i++ {
		key, value, inline := strings.Cut(args[i], "=")
		switch key {
		case "--headless", "--stream-json":
			if inline {
				return options, true, fmt.Errorf("%s takes no value", key)
			}
			streamMode = true
		case "-p", "--print":
			printMode = true
			if inline {
				options.Prompt = value
				promptSet = true
			} else if i+1 < len(args) && (!strings.HasPrefix(args[i+1], "-") || args[i+1] == "-") {
				i++
				options.Prompt = args[i]
				promptSet = true
			}
		case "--input-format", "--output-format", "--resume", "--output-schema":
			if !inline {
				if i+1 == len(args) || strings.HasPrefix(args[i+1], "-") {
					return options, true, fmt.Errorf("%s requires a value", key)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return options, true, fmt.Errorf("%s requires a value", key)
			}
			switch key {
			case "--input-format":
				options.InputFormat = value
			case "--output-format":
				output = value
			case "--resume":
				options.Resume = value
			case "--output-schema":
				options.OutputSchema = value
			}
		case "--verbose", "--include-partial-messages":
			if inline {
				return options, true, fmt.Errorf("%s takes no value", key)
			}
			if key == "--include-partial-messages" {
				options.IncludePartialMessages = true
				partial = true
			}
		case "--":
			if !execMode || promptSet || i+2 != len(args) {
				return options, true, errors.New("exec expects one quoted prompt after --")
			}
			i++
			options.Prompt = args[i]
			promptSet = true
		default:
			if !execMode || promptSet || strings.HasPrefix(args[i], "-") && args[i] != "-" {
				return options, true, fmt.Errorf("unknown headless option or extra argument: %s", args[i])
			}
			options.Prompt = args[i]
			promptSet = true
		}
	}
	if options.InputFormat != "text" && options.InputFormat != "stream-json" {
		return options, true, errors.New("input format must be text or stream-json")
	}
	if streamMode {
		if output != "" && output != "stream-json" {
			return options, true, errors.New("--headless/--stream-json requires stream-json output")
		}
		output = "stream-json"
		options.InputFormat = "stream-json"
	}
	if output == "" {
		output = "text"
	}
	if output != "text" && output != "json" && output != "stream-json" {
		return options, true, errors.New("output format must be text, json or stream-json")
	}
	if output != "stream-json" && options.InputFormat == "stream-json" {
		return options, true, errors.New("stream-json input requires stream-json output")
	}
	if promptSet && options.InputFormat == "stream-json" {
		return options, true, errors.New("a prompt argument cannot be combined with stream-json input")
	}
	if output == "stream-json" && options.OutputSchema != "" {
		return options, true, errors.New("--output-schema requires one-shot text or json output")
	}
	if output != "stream-json" && partial {
		return options, true, errors.New("--include-partial-messages requires stream-json output")
	}
	if !printMode && !streamMode && output == "text" && options.OutputSchema == "" {
		return options, true, errors.New("text output requires -p, --print or exec")
	}
	if promptSet && options.Prompt == "-" {
		options.Prompt = ""
	} else if promptSet && strings.TrimSpace(options.Prompt) == "" {
		return options, true, errors.New("empty prompt")
	}
	options.OutputFormat = output
	return options, true, nil
}

func RunStdio(ctx context.Context, options Options) error {
	if options.OutputFormat != "" && options.OutputFormat != "stream-json" {
		var err error
		options.schema, err = prepareOnce(ctx, options)
		if err != nil {
			return &ExitError{Code: inputExitCode(err), Err: err}
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	backend, err := standard.New()
	if err != nil {
		return err
	}
	defer backend.Close()
	return serveStdio(ctx, backend, cwd, options)
}

func serveStdio(ctx context.Context, backend Backend, cwd string, options Options) error {
	in, out, restore, err := stdioStreams()
	if err != nil {
		return err
	}
	defer restore()
	defer in.Close()
	defer out.Close()
	if options.OutputFormat != "" && options.OutputFormat != "stream-json" {
		return serveOnce(ctx, in, out, backend, cwd, options)
	}
	if options.InputFormat != "stream-json" && options.Prompt == "" {
		stop := context.AfterFunc(ctx, func() { _ = in.Close() })
		data, err := io.ReadAll(io.LimitReader(in, maxFrameSize+1))
		stop()
		if err != nil {
			return err
		}
		if len(data) > maxFrameSize {
			return errors.New("text input exceeds 8 MiB")
		}
		options.Prompt = string(data)
		if strings.TrimSpace(options.Prompt) == "" {
			return errors.New("empty text input")
		}
	}
	return Serve(ctx, in, out, backend, cwd, options)
}
