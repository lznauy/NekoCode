package app

import (
	"testing"
	"time"

	"nekocode/bot/config"
)

func TestApplyConfigReturnsWithoutSelfDeadlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := New()
	done := make(chan error, 1)
	go func() {
		_, err := b.ApplyConfig(config.NewSnapshot(config.Default))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyConfig did not return; likely self-deadlocked during runtime reload")
	}
}
