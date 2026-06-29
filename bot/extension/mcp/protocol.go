package mcp

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func (c *Client) initialize() error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "NekoCode",
			"version": "0.1.0",
		},
	}

	result, err := c.sendRequest("initialize", params)
	if err != nil {
		return err
	}

	var initResp struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initResp); err != nil {
		return fmt.Errorf("parse initialize: %w", err)
	}

	if err := c.sendNotification("notifications/initialized", nil); err != nil {
		return fmt.Errorf("send initialized: %w", err)
	}
	return nil
}

func (c *Client) sendRequest(method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.reqID, 1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if _, err := fmt.Fprintf(c.stdin, "%s\n", body); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	type readResult struct {
		line []byte
		err  error
	}
	readCh := make(chan readResult, 1)
	go func() {
		line, err := c.stdout.ReadBytes('\n')
		readCh <- readResult{line: line, err: err}
	}()

	timeout := c.requestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	var line []byte
	select {
	case result := <-readCh:
		if result.err != nil {
			return nil, fmt.Errorf("read: %w", result.err)
		}
		line = result.line
	case <-time.After(timeout):
		c.stopProcessLockedWithTimeout(100 * time.Millisecond)
		return nil, fmt.Errorf("%s timed out after %s", method, timeout)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.ID != id {
		return nil, fmt.Errorf("response id mismatch: got %d, expected %d", resp.ID, id)
	}

	return resp.Result, nil
}

func (c *Client) sendNotification(method string, params any) error {
	notif := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "%s\n", body)
	return err
}
