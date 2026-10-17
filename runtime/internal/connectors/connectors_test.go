package connectors

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"nekocode/runtime/internal/core"
)

type statusConnector struct {
	name    string
	stopped bool
}

func (c statusConnector) Name() string { return c.name }
func (c statusConnector) Start(context.Context) error {
	return nil
}
func (c *statusConnector) Stop() error {
	c.stopped = true
	return nil
}
func (c statusConnector) HandleCommand(context.Context, []string) (string, error) {
	return "ok", nil
}
func (c *statusConnector) ConnectorStatusView() core.ConnectorView {
	return core.ConnectorView{
		Name:        c.name,
		Configured:  true,
		Running:     true,
		Status:      "running",
		Initialized: true,
	}
}

func TestConnectorManagerStartsConnector(t *testing.T) {
	manager := NewManager(nil)
	manager.Register("telegram", func(core.ConnectorRuntime) Connector {
		return &statusConnector{
			name: "telegram",
		}
	})

	if _, err := manager.Handle(context.Background(), []string{"telegram", "status"}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	view := manager.View()
	var telegram core.ConnectorView
	for _, conn := range view.Connectors {
		if conn.Name == "telegram" {
			telegram = conn
		}
	}
	if !telegram.Initialized {
		t.Fatalf("connector was not initialized: %#v", telegram)
	}
}

func TestConnectorManagerDoesNotAutoStart(t *testing.T) {
	manager := NewManager(nil)
	startCalled := false
	manager.Register("failing", func(core.ConnectorRuntime) Connector {
		return &trackingConnector{name: "failing", started: &startCalled}
	})

	// Lazy init must dispatch the command WITHOUT calling Start: credentials
	// are configured via HandleCommand, so auto-starting would deadlock
	// first-time setup.
	if _, err := manager.Handle(context.Background(), []string{"failing"}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if startCalled {
		t.Fatal("manager auto-started the connector on lazy init")
	}

	view := manager.View()
	for _, conn := range view.Connectors {
		if conn.Name == "failing" && !conn.Initialized {
			t.Fatal("connector should be initialized after first command")
		}
	}
}

type trackingConnector struct {
	name    string
	started *bool
}

func (c trackingConnector) Name() string { return c.name }
func (c trackingConnector) Start(context.Context) error {
	*c.started = true
	return fmt.Errorf("start failed")
}
func (c trackingConnector) Stop() error { return nil }
func (c trackingConnector) HandleCommand(context.Context, []string) (string, error) {
	return "", nil
}

func TestConnectorManagerView(t *testing.T) {
	manager := NewManager(nil)
	manager.Register("telegram", func(core.ConnectorRuntime) Connector {
		return &statusConnector{name: "telegram"}
	})
	manager.Register("slack", func(core.ConnectorRuntime) Connector {
		return &statusConnector{name: "slack"}
	})

	view := manager.View()
	if len(view.Connectors) != 2 {
		t.Fatalf("connectors = %d, want 2", len(view.Connectors))
	}
	if view.Connectors[0].Name != "slack" || view.Connectors[0].Initialized {
		t.Fatalf("first connector before init = %#v", view.Connectors[0])
	}
	if _, err := manager.Handle(context.Background(), []string{"telegram", "status"}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	view = manager.View()
	var telegram core.ConnectorView
	for _, conn := range view.Connectors {
		if conn.Name == "telegram" {
			telegram = conn
		}
	}
	if !telegram.Initialized || !telegram.Running || !telegram.Configured || telegram.Status != "running" {
		t.Fatalf("telegram view = %#v", telegram)
	}
	devices := manager.Devices()
	if devices == "" || !strings.Contains(devices, "telegram") {
		t.Fatalf("devices = %q", devices)
	}
	if msg, err := manager.Disconnect("telegram"); err != nil || !strings.Contains(msg, "stopped") {
		t.Fatalf("Disconnect = %q err=%v", msg, err)
	}
}

// fileConnector is a running connector that records DeliverFile calls.
type fileConnector struct {
	statusConnector
	delivered []string
	fail      bool
}

func (c *fileConnector) DeliverFile(_ context.Context, path string) error {
	if c.fail {
		return fmt.Errorf("boom")
	}
	c.delivered = append(c.delivered, path)
	return nil
}

// stoppedFileConnector reports not-running and supports files.
type stoppedFileConnector struct {
	fileConnector
}

func (c *stoppedFileConnector) ConnectorStatusView() core.ConnectorView {
	return core.ConnectorView{Name: c.name, Status: "stopped"}
}

func TestDeliverFileRoutesByConnectorName(t *testing.T) {
	manager := NewManager(nil)
	tg := &fileConnector{}
	tg.name = "telegram"
	feishuConn := &fileConnector{}
	feishuConn.name = "feishu"
	plain := &statusConnector{name: "qqbot"}
	manager.Register("telegram", func(core.ConnectorRuntime) Connector { return tg })
	manager.Register("feishu", func(core.ConnectorRuntime) Connector { return feishuConn })
	manager.Register("qqbot", func(core.ConnectorRuntime) Connector { return plain })
	// Initialize every connector instance.
	for _, name := range []string{"telegram", "feishu", "qqbot"} {
		if _, err := manager.connector(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}

	// Named delivery hits one connector only.
	sent, err := manager.DeliverFile(context.Background(), "telegram", "/ws/a.zip")
	if err != nil || len(sent) != 1 || sent[0] != "telegram" {
		t.Fatalf("DeliverFile(telegram) = %v err=%v", sent, err)
	}
	if len(tg.delivered) != 1 || len(feishuConn.delivered) != 0 {
		t.Fatalf("delivered: tg=%v feishu=%v", tg.delivered, feishuConn.delivered)
	}

	// Empty name delivers to every file-capable running connector.
	sent, err = manager.DeliverFile(context.Background(), "", "/ws/b.zip")
	if err != nil || len(sent) != 2 {
		t.Fatalf("DeliverFile(all) = %v err=%v", sent, err)
	}

	// Unsupported or unknown connector names report clearly.
	if _, err := manager.DeliverFile(context.Background(), "qqbot", "/ws/c.zip"); err == nil {
		t.Fatal("qqbot has no file support, want error")
	}
	if _, err := manager.DeliverFile(context.Background(), "slack", "/ws/c.zip"); err == nil || !strings.Contains(err.Error(), "unknown connector") {
		t.Fatalf("unknown connector err = %v", err)
	}

	// Not-running connectors are skipped by the broadcast path.
	stopped := &stoppedFileConnector{}
	stopped.name = "wecom"
	manager.Register("wecom", func(core.ConnectorRuntime) Connector { return stopped })
	if _, err := manager.connector(context.Background(), "wecom"); err != nil {
		t.Fatal(err)
	}
	sent, err = manager.DeliverFile(context.Background(), "", "/ws/d.zip")
	if err != nil {
		t.Fatalf("broadcast err = %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("broadcast = %v, want 2 (stopped wecom excluded)", sent)
	}
	if strings.Contains(strings.Join(sent, ","), "wecom") {
		t.Fatalf("stopped connector must be excluded: %v", sent)
	}

	// When a named target fails, its error surfaces.
	feishuConn.fail = true
	if _, err := manager.DeliverFile(context.Background(), "feishu", "/ws/e.zip"); err == nil {
		t.Fatal("failing connector should surface the error")
	}
}
