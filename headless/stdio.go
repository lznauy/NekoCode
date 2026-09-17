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
	selected := false
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")
		switch key {
		case "--headless", "--stream-json", "-p", "--print", "--input-format", "--output-format":
			selected = true
		}
	}
	if !selected {
		return options, false, nil
	}
	output := ""
	for i := 0; i < len(args); i++ {
		key, value, inline := strings.Cut(args[i], "=")
		switch key {
		case "--headless", "--stream-json":
			if inline {
				return options, true, fmt.Errorf("%s takes no value", key)
			}
			options.InputFormat = "stream-json"
			output = "stream-json"
		case "-p", "--print":
			if inline {
				options.Prompt = value
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				options.Prompt = args[i]
			}
		case "--input-format", "--output-format", "--resume":
			if !inline {
				if i+1 == len(args) || strings.HasPrefix(args[i+1], "-") {
					return options, true, fmt.Errorf("%s requires a value", key)
				}
				i++
				value = args[i]
			}
			switch key {
			case "--input-format":
				options.InputFormat = value
			case "--output-format":
				output = value
			case "--resume":
				options.Resume = value
			}
		case "--verbose", "--include-partial-messages":
			if inline {
				return options, true, fmt.Errorf("%s takes no value", key)
			}
			if key == "--include-partial-messages" {
				options.IncludePartialMessages = true
			}
		default:
			return options, true, fmt.Errorf("unknown headless option: %s", key)
		}
	}
	if output != "stream-json" {
		return options, true, errors.New("headless mode requires --output-format stream-json (or --headless)")
	}
	if options.InputFormat != "text" && options.InputFormat != "stream-json" {
		return options, true, errors.New("input format must be text or stream-json")
	}
	if options.Prompt != "" && options.InputFormat == "stream-json" {
		return options, true, errors.New("a prompt argument cannot be combined with stream-json input")
	}
	return options, true, nil
}

func RunStdio(ctx context.Context, options Options) error {
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
