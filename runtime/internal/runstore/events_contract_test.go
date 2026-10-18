package runstore_test

import (
	"testing"

	"nekocode/internal/testutil/eventcontract"
	"nekocode/runtime/internal/runstore"
)

func TestSharedLifecycleContract(t *testing.T) {
	for _, scenario := range eventcontract.Scenarios() {
		t.Run(scenario.Name, func(t *testing.T) {
			store := runstore.NewRunStore(10)
			for _, event := range scenario.Events {
				store.Record(event)
			}
			snapshot, ok := store.Lookup("contract-run")
			if !ok || snapshot.Output != scenario.SnapshotOutput {
				t.Fatalf("snapshot=%+v wantOutput=%q", snapshot, scenario.SnapshotOutput)
			}
		})
	}
}
