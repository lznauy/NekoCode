package qqbot

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// QQ 机器人 gateway op code。
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11
)

// 订阅意图：群 @ 与 C2C 私聊（1<<25）+ 公域频道 @（1<<30）。
const intents = 1<<25 | 1<<30

// wsFrame 是 gateway 数据帧：op 操作码、t 事件名（Dispatch 时）、
// s 序号（心跳时回传）、d 载荷。
type wsFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  int             `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// parseFrame 解析一帧 gateway 数据。
func parseFrame(data []byte) (wsFrame, bool) {
	var f wsFrame
	if err := json.Unmarshal(data, &f); err != nil {
		return wsFrame{}, false
	}
	return f, true
}

// helloInterval 读取 Hello（op 10）携带的心跳间隔（毫秒）。
func (f wsFrame) helloInterval() (int, bool) {
	var d struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(f.D, &d); err != nil || d.HeartbeatInterval <= 0 {
		return 0, false
	}
	return d.HeartbeatInterval, true
}

// readySessionID 读取 READY 事件的 session_id（重连 Resume 时用）。
func (f wsFrame) readySessionID() string {
	var d struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(f.D, &d); err != nil {
		return ""
	}
	return d.SessionID
}

// invalidSessionResumable 读取 Invalid Session（op 9）的 d：true 可 Resume，
// false 需重新 Identify。d 缺失时按不可恢复处理。
func (f wsFrame) invalidSessionResumable() bool {
	var resumable bool
	if err := json.Unmarshal(f.D, &resumable); err != nil {
		return false
	}
	return resumable
}

// gatewaySession 维护一次或多次 gateway 连接的生命周期；sessionID/lastSeq
// 跨重连保留，以便 Resume 续传。
type gatewaySession struct {
	client *apiClient

	writeMu   sync.Mutex // 写必须串行化（心跳与 Identify/Resume 并发）
	sessionID string
	lastSeq   atomic.Int64
}

// run 完成一次连接生命周期：取 gateway 地址 → dial → Hello/Identify 或
// Resume → 读循环分发事件。返回错误时由外层退避重连；ctx 取消则退出。
// onMessage 投递消息事件，onReady 在 READY 时回调。
func (s *gatewaySession) run(ctx context.Context, onMessage func(inboundMessage), onReady func()) error {
	wsURL, err := s.client.gatewayURL(ctx)
	if err != nil {
		return err
	}
	token, err := s.client.accessToken(ctx)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	connectionCtx, cancelConnection := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancelConnection()
		_ = conn.Close()
		workers.Wait()
	}()
	workers.Go(func() {
		<-connectionCtx.Done()
		_ = conn.Close()
	})

	stopHB := connectionCtx.Done()
	hbStarted := false

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		frame, ok := parseFrame(data)
		if !ok {
			continue
		}
		switch frame.Op {
		case opHello:
			interval, ok := frame.helloInterval()
			if !ok {
				return errors.New("gateway hello missing heartbeat_interval")
			}
			if !hbStarted {
				workers.Go(func() { s.heartbeatLoop(connectionCtx, conn, interval, stopHB) })
				hbStarted = true
			}
			if err := s.identifyOrResume(conn, token); err != nil {
				return err
			}
		case opDispatch:
			if frame.S != 0 {
				s.lastSeq.Store(int64(frame.S))
			}
			switch frame.T {
			case "READY":
				if id := frame.readySessionID(); id != "" {
					s.sessionID = id
				}
				if onReady != nil {
					onReady()
				}
			case "GROUP_AT_MESSAGE_CREATE", "C2C_MESSAGE_CREATE":
				if msg, ok := messageFromEvent(frame.T, frame.D); ok {
					onMessage(msg)
				}
				// AT_MESSAGE_CREATE（频道 @）v1 不处理。
			}
		case opHeartbeatACK:
			// 心跳确认，无需处理。
		case opReconnect:
			return errors.New("gateway requested reconnect")
		case opInvalidSession:
			if frame.invalidSessionResumable() && s.sessionID != "" {
				if err := s.resume(conn, token); err != nil {
					return err
				}
			} else {
				s.sessionID = ""
				s.lastSeq.Store(0)
				if err := s.identify(conn, token); err != nil {
					return err
				}
			}
		}
	}
}

// identifyOrResume 有会话则 Resume 续传，否则重新 Identify。
func (s *gatewaySession) identifyOrResume(conn *websocket.Conn, token string) error {
	if s.sessionID != "" {
		return s.resume(conn, token)
	}
	return s.identify(conn, token)
}

func (s *gatewaySession) identify(conn *websocket.Conn, token string) error {
	return s.writeJSON(conn, map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":      "QQBot " + token,
			"intents":    intents,
			"shard":      []int{0, 1},
			"properties": map[string]any{},
		},
	})
}

func (s *gatewaySession) resume(conn *websocket.Conn, token string) error {
	return s.writeJSON(conn, map[string]any{
		"op": opResume,
		"d": map[string]any{
			"token":      "QQBot " + token,
			"session_id": s.sessionID,
			"seq":        s.lastSeq.Load(),
		},
	})
}

// heartbeatLoop 按 Hello 给出的间隔发心跳（首次在 interval * random(0~1)
// 后），d 为最后收到的事件序号（无则 null）。
func (s *gatewaySession) heartbeatLoop(ctx context.Context, conn *websocket.Conn, intervalMs int, stop <-chan struct{}) {
	interval := time.Duration(intervalMs) * time.Millisecond
	select {
	case <-ctx.Done():
		return
	case <-stop:
		return
	case <-time.After(time.Duration(rand.Float64() * float64(interval))):
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			var d any
			if seq := s.lastSeq.Load(); seq != 0 {
				d = seq
			}
			if err := s.writeJSON(conn, map[string]any{"op": opHeartbeat, "d": d}); err != nil {
				return
			}
		}
	}
}

func (s *gatewaySession) writeJSON(conn *websocket.Conn, v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteJSON(v)
}
