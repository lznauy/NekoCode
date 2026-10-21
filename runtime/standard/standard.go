// Package standard assembles NekoCode's standard runtime with the default bot,
// event recording and bundled connectors.
package standard

import (
	"context"
	"errors"
	"fmt"

	"nekocode/bot/core"
	"nekocode/bot/extension/tool/builtin/deliver"
	"nekocode/interaction/connect/feishu"
	"nekocode/interaction/connect/qqbot"
	"nekocode/interaction/connect/telegram"
	"nekocode/interaction/connect/wecom"
	controlruntime "nekocode/runtime"
)

// New constructs the standard application runtime.
func New() (*controlruntime.Runtime, error) {
	standardBot, err := core.New()
	if err != nil {
		return nil, fmt.Errorf("standard runtime: %w", err)
	}
	rt := FromBot(standardBot)
	// The deliver_file tool bridges model → IM chats through the runtime's
	// connector manager; the bot itself stays connector-agnostic.
	standardBot.RegisterExtraTools(deliver.NewTool(runtimeDeliverer{rt: rt}))
	if err := rt.EnableDefaultEventRecording(); err != nil {
		return nil, errors.Join(fmt.Errorf("standard runtime: %w", err), rt.Close())
	}
	rt.RegisterConnector("telegram", func(runtime controlruntime.ConnectorRuntime) controlruntime.Connector {
		return telegram.New(runtime)
	})
	rt.RegisterConnector("feishu", func(runtime controlruntime.ConnectorRuntime) controlruntime.Connector {
		return feishu.New(runtime)
	})
	rt.RegisterConnector("qqbot", func(runtime controlruntime.ConnectorRuntime) controlruntime.Connector {
		return qqbot.New(runtime)
	})
	rt.RegisterConnector("wecom", func(runtime controlruntime.ConnectorRuntime) controlruntime.Connector {
		return wecom.New(runtime)
	})
	return rt, nil
}

// runtimeDeliverer adapts the runtime's connector file delivery into the
// deliver tool's injection interface.
type runtimeDeliverer struct {
	rt *controlruntime.Runtime
}

func (d runtimeDeliverer) DeliverConnectorFile(ctx context.Context, connector, path string) ([]string, error) {
	return d.rt.DeliverConnectorFile(ctx, connector, path)
}

// FromBot creates a runtime around an existing standard bot without enabling
// event recording or registering bundled connectors.
func FromBot(standardBot *core.Bot) *controlruntime.Runtime {
	if standardBot == nil {
		panic("runtime/standard: nil bot")
	}
	adapter := adapt(standardBot)
	rt := controlruntime.New(adapter, adapter.services())
	// Background MCP authorization outcomes surface as system messages in
	// every transport, not only where the user happened to run /mcp-login.
	standardBot.SetMCPAuthNotifier(rt.Notify)
	return rt
}
