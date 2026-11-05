package types

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	nethttp "net/http"
	"strings"

	utilhttp "nekocode/util/http"
	"nekocode/util/sse"
)

// ErrStreamDone lets provider-specific parsers report a terminal SSE event
// without treating normal stream completion as an error.
var ErrStreamDone = errors.New("stream complete")

// DoJSONRequest marshals body to JSON, sends an HTTP POST with the given
// headers, and returns the raw response bytes. Callers handle their own
// unmarshaling since different providers use different response shapes.
func DoJSONRequest(ctx context.Context, url string, headers map[string]string, body any) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := SharedHTTPClientTimeout.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != nethttp.StatusOK {
		return nil, utilhttp.NewHTTPError(resp.StatusCode, string(data))
	}
	return data, nil
}

// StreamSSE reads an SSE stream from resp, extracts data payloads, and calls
// parseEvent for each one. Errors are sent to errCh (buffered, size 1).
// A terminal SSE event is required; complete may additionally accept a clean
// EOF after a provider-specific finish marker while retaining usage tail events.
// This function runs synchronously and blocks until the stream ends;
// the caller is responsible for launching it in a goroutine and managing
// the channel lifetimes (close tokenCh/errCh when this returns).
func StreamSSE(
	ctx context.Context,
	resp *nethttp.Response,
	tokenCh chan<- StreamToken,
	errCh chan<- error,
	parseEvent func(data string, tokenCh chan<- StreamToken) error,
	complete ...func() bool,
) {
	defer func() { _ = resp.Body.Close() }()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = resp.Body.Close()
		case <-done:
		}
	}()

	if resp.StatusCode != nethttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errCh <- utilhttp.NewHTTPError(resp.StatusCode, string(body))
		return
	}

	const maxEventBytes = 1024 * 1024
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(splitSSELine)
	scanner.Buffer(make([]byte, 0, 64*1024), maxEventBytes)
	var event strings.Builder
	hasData := false
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
			firstLine = false
		}
		if line != "" {
			data, ok := sse.SSELineData(line)
			if !ok {
				continue
			}
			extra := len(data)
			if hasData {
				extra++
			}
			if extra > maxEventBytes-event.Len() {
				errCh <- errors.New("SSE event exceeds 1 MiB limit")
				return
			}
			if hasData {
				event.WriteByte('\n')
			}
			event.WriteString(data)
			hasData = true
			continue
		}
		if !hasData {
			continue
		}
		data := event.String()
		event.Reset()
		hasData = false
		if data == "[DONE]" {
			return
		}
		if err := parseEvent(data, tokenCh); err != nil {
			if errors.Is(err, ErrStreamDone) {
				return
			}
			errCh <- err
			return
		}
	}
	// SSE dispatches only at a blank line; an unfinished event is discarded.

	if ctx.Err() != nil {
		errCh <- ctx.Err()
		return
	}
	if err := scanner.Err(); err != nil {
		errCh <- err
		return
	}
	if len(complete) == 0 || !complete[0]() {
		errCh <- io.ErrUnexpectedEOF
	}
}

// splitSSELine recognizes all SSE line endings: LF, CRLF, and bare CR.
func splitSSELine(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i], nil
		}
		if b == '\r' {
			if i+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			advance = i + 1
			if advance < len(data) && data[advance] == '\n' {
				advance++
			}
			return advance, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// SendStreamToken lets a canceled consumer stop reading without stranding the producer.
func SendStreamToken(ctx context.Context, out chan<- StreamToken, token StreamToken) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case out <- token:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StreamError preserves the provider error payload and maps its error type to
// the HTTP status contract consumed by retry classification.
func StreamError(data string) error {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(data), &envelope) != nil || len(envelope.Error) == 0 || string(envelope.Error) == "null" {
		return nil
	}
	var detail struct {
		Type string `json:"type"`
		Code string `json:"code"`
	}
	_ = json.Unmarshal(envelope.Error, &detail)
	status := 400
	for _, code := range []string{detail.Type, detail.Code} {
		switch code {
		case "rate_limit_error", "rate_limit_exceeded":
			status = 429
		case "overloaded_error":
			status = 529
		case "api_error", "server_error", "internal_server_error":
			status = 500
		case "authentication_error", "invalid_api_key":
			status = 401
		case "permission_error":
			status = 403
		case "not_found_error":
			status = 404
		case "request_too_large":
			status = 413
		}
	}
	return utilhttp.NewHTTPError(status, data)
}
