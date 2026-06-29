package types

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"nekocode/common"
)

// DoJSONRequest marshals body to JSON, sends an HTTP POST with the given
// headers, and returns the raw response bytes. Callers handle their own
// unmarshaling since different providers use different response shapes.
func DoJSONRequest(ctx context.Context, url string, headers map[string]string, body any) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
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
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, common.NewHTTPError(resp.StatusCode, string(data))
	}
	return data, nil
}

// StreamSSE reads an SSE stream from resp, extracts data payloads, and calls
// parseEvent for each one. Errors are sent to errCh (buffered, size 1).
// This function runs synchronously and blocks until the stream ends;
// the caller is responsible for launching it in a goroutine and managing
// the channel lifetimes (close tokenCh/errCh when this returns).
func StreamSSE(
	ctx context.Context,
	resp *http.Response,
	tokenCh chan<- StreamToken,
	errCh chan<- error,
	parseEvent func(data string, tokenCh chan<- StreamToken) error,
) {
	defer resp.Body.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			resp.Body.Close()
		case <-done:
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errCh <- common.NewHTTPError(resp.StatusCode, string(body))
		close(done)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer for large SSE payloads
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := common.SSELineData(line)
		if !ok {
			continue
		}
		if data == "[DONE]" {
			continue
		}
		if err := parseEvent(data, tokenCh); err != nil {
			errCh <- err
			break
		}
	}
	close(done)
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			errCh <- ctx.Err()
		} else {
			errCh <- err
		}
	}
}
