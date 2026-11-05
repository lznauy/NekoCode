package feishu

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

type rejectImageRuntime struct {
	controlruntime.ConnectorRuntime
	path string
}

func (*rejectImageRuntime) CommandMenu(context.Context, string) (protocol.CommandMenu, bool) {
	return protocol.CommandMenu{}, false
}
func (*rejectImageRuntime) ExecuteLocalCommand(context.Context, string) (string, controlruntime.LocalCommandResult) {
	return "", controlruntime.LocalCommandNotCommand
}
func (s *rejectImageRuntime) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	s.path = input.Images[0].Path
	return "", errors.New("runtime busy")
}
func TestRejectedImageReleasesDownload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{AppID: "image-test", AppSecret: "secret"}
	cfg.finishPairing("owner", "chat")
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"tenant_access_token":"local-test-token","expire":7200}`))
		case strings.Contains(r.URL.Path, "/resources/"):
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="image.png"`)
			w.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"data":{"message_id":"reply"}}`))
		}
	}))
	defer server.Close()
	client := newFeishuClient(cfg.AppID, cfg.AppSecret)
	client.api = lark.NewClient(cfg.AppID, cfg.AppSecret, lark.WithOpenBaseUrl(server.URL), lark.WithHttpClient(server.Client()))
	rt := &rejectImageRuntime{}
	c := New(rt)
	c.api = client
	c.handleMessage(t.Context(), client, &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
		Sender:  &larkim.EventSender{SenderId: &larkim.UserId{OpenId: strPtr("owner")}},
		Message: &larkim.EventMessage{MessageId: strPtr("msg"), ChatId: strPtr("chat"), MessageType: strPtr("image"), Content: strPtr(`{"image_key":"image"}`)},
	}})
	if rt.path == "" {
		t.Fatal("download did not reach runtime")
	}
	defer os.Remove(rt.path)
	if _, err := os.Stat(rt.path); !os.IsNotExist(err) {
		t.Fatalf("rejected download retained: %v", err)
	}
}
