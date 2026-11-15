package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"nekocode/interaction/tui/components"
	"nekocode/util/attachment"

	tea "charm.land/bubbletea/v2"
)

type clipboardImageMsg struct {
	scope clipboardScope
	data  []byte
	text  string
	err   error
}

type clipboardScope struct {
	sessionID            string
	sessionGeneration    uint64
	submissionGeneration uint64
	input                *components.Input
	draftGeneration      uint64
}

func (m *Model) clipboardScope() clipboardScope {
	scope := clipboardScope{sessionGeneration: m.sessionLoadGeneration, submissionGeneration: m.clipboardGeneration, input: m.Input, draftGeneration: m.Input.DraftGeneration()}
	if source, ok := m.Runtime.(interface{ CurrentSessionID() string }); ok {
		scope.sessionID = source.CurrentSessionID()
	}
	return scope
}

const maxClipboardTextBytes = 1 << 20

var errClipboardNoData = errors.New("clipboard contains no matching data")

func (m *Model) readClipboardImage() tea.Cmd {
	// Capture on the event loop, before the asynchronous command starts.
	scope := m.clipboardScope()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		data, err := readClipboardIsolated(ctx, clipboardHelperImage, attachment.MaxImageBytes)
		if errors.Is(err, errClipboardNoData) {
			textData, textErr := readClipboardIsolated(ctx, clipboardHelperText, maxClipboardTextBytes)
			if errors.Is(textErr, errClipboardNoData) {
				return clipboardImageMsg{scope: scope, err: errors.New("剪贴板中没有可读取的图片或文本；请先复制截图，再按 Alt+V")}
			}
			if textErr != nil {
				return clipboardImageMsg{scope: scope, err: textErr}
			}
			return clipboardImageMsg{scope: scope, text: string(textData)}
		}
		return clipboardImageMsg{scope: scope, data: data, err: err}
	}
}

func readClipboardIsolated(ctx context.Context, format string, limit int64) ([]byte, error) {
	if format != clipboardHelperImage && format != clipboardHelperText {
		return nil, fmt.Errorf("unsupported clipboard format %q", format)
	}
	// WSL's Linux display clipboard may not contain Windows screenshots.
	// Read the host clipboard directly instead of starting a Linux helper.
	if useWindowsClipboard() {
		return readWindowsClipboard(ctx, format, limit)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable)
	cmd.Env = append(os.Environ(), clipboardHelperEnv+"="+format, "GOMEMLIMIT=64MiB")
	return readClipboardCommand(ctx, cmd, format, limit)
}

func readClipboardCommand(ctx context.Context, cmd *exec.Cmd, format string, limit int64) ([]byte, error) {
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, tooLarge, readErr := readBounded(stdout, limit)
	if tooLarge && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if tooLarge {
		return nil, fmt.Errorf("clipboard %s exceeds %d MiB limit", format, limit>>20)
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("读取剪贴板超时或已取消：%w", ctx.Err())
	}
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == clipboardHelperNoDataExit {
			return nil, errClipboardNoData
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, errors.New(message)
		}
		return nil, waitErr
	}
	if len(data) == 0 {
		return nil, errClipboardNoData
	}
	return data, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return nil, true, nil
	}
	return data, false, nil
}
