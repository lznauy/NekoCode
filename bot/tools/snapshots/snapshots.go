package snapshots

import (
	"context"

	"nekocode/bot/tools/editdsl"
	"nekocode/bot/tools/execution"
)

func Record(store *editdsl.SnapshotStore, path, content string) string {
	if store == nil {
		return ""
	}
	return store.Record(path, content)
}

func RecordInContext(ctx context.Context, path, content string) string {
	return Record(execution.SnapshotStoreFromContext(ctx), path, content)
}
