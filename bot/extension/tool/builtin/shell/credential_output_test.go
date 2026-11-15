package shell

import (
	"nekocode/util/secrets"
	"strings"
	"testing"
)

func TestProcessOutputRedactsLoadedCredentials(t *testing.T) {
	key := "credential-shell-sentinel-987654321"
	secrets.Register(key)
	task := &shellTask{changed: make(chan struct{})}
	drainToRing(task, strings.NewReader("before "+key+" after"))
	if got := string(task.logs); strings.Contains(got, key) || !strings.Contains(got, secrets.Replacement) {
		t.Fatalf("unprotected process output %q", got)
	}
}
