package tools

import (
	"context"
	"net/http"
	"time"

	"nekocode/bot/tools/netclient"
	"nekocode/bot/tools/pathutil"
	"nekocode/bot/tools/semantics"
	"nekocode/bot/tools/snapshots"
	"nekocode/bot/tools/textutil"
)

func StripAnsi(s string) string {
	return textutil.StripAnsi(s)
}

func ValidatePath(path string) (string, error) {
	return pathutil.ValidatePath(path)
}

func NormalizeText(text string) string {
	return textutil.NormalizeText(text)
}

func ReadSafeFile(path string) ([]byte, error) {
	return pathutil.ReadSafeFile(path)
}

func NewToolHTTPClient(timeout time.Duration) *http.Client {
	return netclient.NewHTTPClient(timeout)
}

func IsAllExploratory(calls []ToolCallItem) bool {
	return semantics.IsAllExploratory(calls)
}

func RecordSnapshot(path, content string) string {
	return snapshots.Record(GetGlobalSnapshotStore(), path, content)
}

func RecordSnapshotInContext(ctx context.Context, path, content string) string {
	return snapshots.RecordInContext(ctx, path, content)
}
