//go:build linux

package impl

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This helper is executed twice inside the same sandbox command: a persistent
// server and a separate client process. It is never part of an ordinary test run.
func TestLocalServiceProbe(t *testing.T) {
	mode := os.Getenv("NEKOCODE_LOCAL_SERVICE_PROBE")
	if mode == "" {
		t.Skip("sandbox helper only")
	}
	readiness := os.Getenv("NEKOCODE_LOCAL_SERVICE_READY")
	if mode == "server" {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(readiness, []byte("http://"+listener.Addr().String()), 0600); err != nil {
			t.Fatal(err)
		}
		_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "service-ready-http-ok") }))
		return
	}
	url, err := os.ReadFile(readiness)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	response, err := client.Get(string(url))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "service-ready-http-ok" {
		t.Fatalf("HTTP probe: %s %v", data, err)
	}
	fmt.Println(string(data))
}

func TestNativeLocalServiceReadinessAndClientInOneCommand(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("native sandbox unavailable")
	}
	workspace := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(workspace, "probe")
	if err = os.WriteFile(probe, data, 0700); err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.ReplaceAll(probe, "'", "'\\''") + "'"
	command := `export NEKOCODE_LOCAL_SERVICE_READY="$PWD/ready"
NEKOCODE_LOCAL_SERVICE_PROBE=server ` + quoted + ` -test.run='^TestLocalServiceProbe$' &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null; wait "$server_pid" 2>/dev/null' EXIT
for attempt in {1..100}; do
 test -s "$NEKOCODE_LOCAL_SERVICE_READY" && break
 kill -0 "$server_pid" 2>/dev/null || exit 1
 sleep 0.02
done
test -s "$NEKOCODE_LOCAL_SERVICE_READY" || exit 1
NEKOCODE_LOCAL_SERVICE_PROBE=client ` + quoted + ` -test.run='^TestLocalServiceProbe$' -test.v`
	out, err := runNativeBash(t.Context(), command, Profile{Workspace: workspace}, 10*time.Second)
	if err != nil || !strings.Contains(out, "service-ready-http-ok") {
		t.Fatalf("local server/client command: %v\n%s", err, out)
	}
}
