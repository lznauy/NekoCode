package runtime

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

type deliveryRouteConnector struct {
	name  string
	calls atomic.Int32
}

func (c *deliveryRouteConnector) Name() string                { return c.name }
func (c *deliveryRouteConnector) Start(context.Context) error { return nil }
func (c *deliveryRouteConnector) Stop() error                 { return nil }
func (c *deliveryRouteConnector) HandleCommand(context.Context, []string) (string, error) {
	return "", nil
}
func (c *deliveryRouteConnector) DeliverFile(context.Context, string) error {
	c.calls.Add(1)
	return nil
}

func TestDeliveryRoutesFromActualRunContext(t *testing.T) {
	type result struct {
		source SourceRef
		sent   []string
		err    error
	}
	results := make(chan result, 1)
	var rt *Runtime
	rt = New(RunnerFunc(func(ctx context.Context, target string, _ RunHost) (string, error) {
		if target == "default" {
			target = ""
		}
		// Match Agent.getCtx: lifecycle cancellation + execution identity,
		// without arbitrary values from the runner's incoming context.
		toolCtx := protocol.WithExecutionIdentity(context.Background(), protocol.ExecutionFromContext(ctx))
		sent, err := rt.DeliverConnectorFile(toolCtx, target, "/local/snapshot.zip")
		results <- result{core.RunSourceFromContext(ctx), sent, err}
		return "done", nil
	}), ServiceGroups{})
	defer rt.Close()
	tg, fs := &deliveryRouteConnector{name: "telegram"}, &deliveryRouteConnector{name: "feishu"}
	for _, conn := range []*deliveryRouteConnector{tg, fs} {
		rt.RegisterConnector(conn.name, func(ConnectorRuntime) Connector { return conn })
		if _, err := rt.Connect(t.Context(), conn.name, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		source SourceRef
		target string
		want   []string
	}{
		{"reply to telegram only", SourceRef{Kind: "telegram", ID: "123"}, "default", []string{"telegram"}},
		{"reply to feishu only", SourceRef{Kind: "feishu", ID: "chat"}, "default", []string{"feishu"}},
		{"local needs target", SourceRef{Kind: "tui"}, "default", nil},
		{"no source needs target", SourceRef{}, "default", nil},
		{"source cannot request broadcast", SourceRef{Kind: "*"}, "default", nil},
		{"explicit target", SourceRef{Kind: "tui"}, "feishu", []string{"feishu"}},
		{"explicit broadcast", SourceRef{Kind: "tui"}, "*", []string{"feishu", "telegram"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeTG, beforeFS := tg.calls.Load(), fs.calls.Load()
			caller := core.WithRunSource(t.Context(), SourceRef{Kind: "wrong-caller-source"})
			id, err := rt.StartRun(caller, Input{Source: tc.source, Text: tc.target})
			if err != nil {
				t.Fatal(err)
			}
			waitForRun(t, rt, id)
			got := <-results
			if got.source != tc.source || !reflect.DeepEqual(got.sent, tc.want) || (got.err != nil) != (tc.want == nil) {
				t.Fatalf("route=%+v want=%v", got, tc.want)
			}
			if tc.want == nil && (tg.calls.Load() != beforeTG || fs.calls.Load() != beforeFS) {
				t.Fatal("ambiguous default sent a file")
			}
		})
	}
}
