package main

import (
	"context"
	"strings"
	"testing"
)

type bootstrapCall struct {
	name string
	args []string
}

type bootstrapRuntime struct {
	calls []bootstrapCall
}

func (r *bootstrapRuntime) Connect(_ context.Context, name string, args []string) (string, error) {
	r.calls = append(r.calls, bootstrapCall{name: name, args: append([]string(nil), args...)})
	return name + " ready", nil
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestBootstrapConnectorsDisabledWithoutConfiguration(t *testing.T) {
	rt := &bootstrapRuntime{}
	statuses, err := bootstrapConnectors(context.Background(), rt.Connect, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 || len(rt.calls) != 0 {
		t.Fatalf("statuses=%v calls=%v, want disabled bootstrap", statuses, rt.calls)
	}
}

func TestValidateListenSecurity(t *testing.T) {
	for _, test := range []struct {
		addr    string
		token   string
		wantErr bool
	}{
		{addr: "127.0.0.1:8765"},
		{addr: "[::1]:8765"},
		{addr: "localhost:8765", wantErr: true},
		{addr: "192.168.1.10:8765", wantErr: true},
		{addr: "network-host.invalid:8765", wantErr: true},
		{addr: "0.0.0.0:8765", wantErr: true},
		{addr: ":8765", wantErr: true},
		{addr: "[::]:8765", token: "secret"},
	} {
		err := validateListenSecurity(test.addr, test.token)
		if (err != nil) != test.wantErr {
			t.Errorf("validateListenSecurity(%q, %q) error = %v, wantErr %v", test.addr, test.token, err, test.wantErr)
		}
	}
}

func TestBootstrapConnectorsSelectsTelegram(t *testing.T) {
	rt := &bootstrapRuntime{}
	statuses, err := bootstrapConnectors(context.Background(), rt.Connect, env(map[string]string{
		"NEKOCODE_CONNECTORS":         "telegram",
		"NEKOCODE_TELEGRAM_BOT_TOKEN": " token ",
		"NEKOCODE_FEISHU_APP_ID":      "ignored",
		"NEKOCODE_FEISHU_APP_SECRET":  "ignored",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].name != "telegram" {
		t.Fatalf("statuses = %#v", statuses)
	}
	wantCalls := []bootstrapCall{
		{name: "telegram", args: []string{"add", "token"}},
		{name: "telegram", args: nil},
	}
	assertBootstrapCalls(t, rt.calls, wantCalls)
}

func TestBootstrapConnectorsSupportsMultipleTransports(t *testing.T) {
	rt := &bootstrapRuntime{}
	statuses, err := bootstrapConnectors(context.Background(), rt.Connect, env(map[string]string{
		"NEKOCODE_CONNECTORS":        "feishu, qqbot,feishu",
		"NEKOCODE_FEISHU_APP_ID":     "cli_app",
		"NEKOCODE_FEISHU_APP_SECRET": "secret",
		"NEKOCODE_QQBOT_APP_ID":      "qq_app",
		"NEKOCODE_QQBOT_APP_SECRET":  "qq_secret",
		"NEKOCODE_QQBOT_SANDBOX":     "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].name != "feishu" || statuses[1].name != "qqbot" {
		t.Fatalf("statuses = %#v", statuses)
	}
	wantCalls := []bootstrapCall{
		{name: "feishu", args: []string{"add", "cli_app", "secret"}},
		{name: "feishu", args: nil},
		{name: "qqbot", args: []string{"sandbox", "on"}},
		{name: "qqbot", args: []string{"add", "qq_app", "qq_secret"}},
	}
	assertBootstrapCalls(t, rt.calls, wantCalls)
}

func TestBootstrapConnectorsStartsPersistedConfiguration(t *testing.T) {
	rt := &bootstrapRuntime{}
	_, err := bootstrapConnectors(context.Background(), rt.Connect, env(map[string]string{
		"NEKOCODE_CONNECTORS": "feishu,telegram,qqbot",
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []bootstrapCall{
		{name: "feishu", args: nil},
		{name: "telegram", args: nil},
		{name: "qqbot", args: []string{"start"}},
	}
	assertBootstrapCalls(t, rt.calls, wantCalls)
}

func TestBootstrapConnectorsConfiguresWeCom(t *testing.T) {
	rt := &bootstrapRuntime{}
	statuses, err := bootstrapConnectors(context.Background(), rt.Connect, env(map[string]string{
		"NEKOCODE_WECOM_BOT_ID":     " bot-id ",
		"NEKOCODE_WECOM_BOT_SECRET": " secret ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].name != "wecom" {
		t.Fatalf("statuses = %#v", statuses)
	}
	assertBootstrapCalls(t, rt.calls, []bootstrapCall{
		{name: "wecom", args: []string{"add", "bot-id", "secret"}},
		{name: "wecom", args: nil},
	})
}

func TestBootstrapConnectorsRejectsInvalidConfiguration(t *testing.T) {
	for _, values := range []map[string]string{
		{"NEKOCODE_CONNECTORS": "slack"},
		{"NEKOCODE_CONNECTORS": "feishu", "NEKOCODE_FEISHU_APP_ID": "cli_app"},
		{"NEKOCODE_CONNECTORS": "qqbot", "NEKOCODE_QQBOT_SANDBOX": "maybe"},
	} {
		rt := &bootstrapRuntime{}
		if _, err := bootstrapConnectors(context.Background(), rt.Connect, env(values)); err == nil {
			t.Fatalf("bootstrapConnectors(%v) succeeded", values)
		}
	}
}

func assertBootstrapCalls(t *testing.T, got, want []bootstrapCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i].name != want[i].name || strings.Join(got[i].args, "\x00") != strings.Join(want[i].args, "\x00") {
			t.Fatalf("call[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
