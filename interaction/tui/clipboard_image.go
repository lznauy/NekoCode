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

	"nekocode/util/attachment"

	tea "charm.land/bubbletea/v2"
)

type clipboardImageMsg struct {
	data []byte
	err  error
}

const maxClipboardTextBytes = 1 << 20

var errClipboardNoData = errors.New("clipboard contains no matching data")

func readClipboardImage() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		data, err := readClipboardIsolated(ctx, clipboardHelperImage, attachment.MaxImageBytes)
		if errors.Is(err, errClipboardNoData) {
			textData, textErr := readClipboardIsolated(ctx, clipboardHelperText, maxClipboardTextBytes)
			if errors.Is(textErr, errClipboardNoData) {
				return clipboardImageMsg{}
			}
			if textErr != nil {
				return clipboardImageMsg{err: textErr}
			}
			return tea.PasteMsg{Content: string(textData)}
		}
		return clipboardImageMsg{data: data, err: err}
	}
}

func readClipboardIsolated(ctx context.Context, format string, limit int64) ([]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable)
	cmd.Env = append(os.Environ(), clipboardHelperEnv+"="+format, "GOMEMLIMIT=64MiB")
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
