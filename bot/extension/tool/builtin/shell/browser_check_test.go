package shell

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBrowserCheckInLocalShell(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux shell integration")
	}
	for _, name := range []string{"python3", "node", "google-chrome"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s unavailable", name)
		}
	}
	tool := testShellTool()
	tool.initialWait = 30 * time.Second
	cleanupShellTool(t, tool)
	prior, err := tool.ExecuteWithPermission(t.Context(), map[string]any{"command": `printf 'stale fixture' > "$NEKOCODE_SCRATCH/browser-check.mjs"`}, permReq())
	if err != nil || !prior.Process.ExitCodeKnown || prior.Process.ExitCode != 0 {
		t.Fatalf("prepare scratch fixture: %+v, %v", prior, err)
	}
	const command = `set -eu
browser_check() { node --input-type=module -e "${NEKOCODE_BROWSER_CHECK:?Linux sandbox required}" -- "$@"; }
fixture=$(mktemp -d)
cd "$fixture"
echo '<button onclick="this.textContent=Number(this.textContent)+1">0</button>' > index.html
python3 -u -c 'from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler
class Handler(SimpleHTTPRequestHandler):
    warming = True
    def do_GET(self):
        if self.path == "/" and Handler.warming:
            Handler.warming = False
            self.send_error(503)
        else:
            super().do_GET()
s=ThreadingHTTPServer(("127.0.0.1",0),Handler)
print(s.server_port,flush=True)
s.serve_forever()' >port 2>server.log &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; rm -rf "$fixture"' EXIT
for i in {1..50}; do test -s port && break; sleep .05; done
url="http://127.0.0.1:$(cat port)/"
browser_check "$url" 'do:document.querySelector("button").click()' 'document.querySelector("button").textContent==="1"'
browser_check "$url" 'do:setTimeout(() => { const el = document.createElement("span"); el.id = "late"; el.textContent = "ready"; document.body.append(el) }, 500)' 'document.querySelector("#late").textContent==="ready"'
if browser_check "$url" 'do:if (!window.attempted) { window.attempted = true; throw Error("broken interaction") }' 'true'; then exit 1; fi
if browser_check "$url" 'document.querySelector("#absent").textContent==="ready"' 2>assertion.log; then exit 1; fi
grep -q 'Assertion 1 timed out.*TypeError' assertion.log
if browser_check "$url" 'do:document.title' 2>action.log; then exit 1; fi
grep -q 'at least one page assertion' action.log
if browser_check "${url}missing" 'true'; then exit 1; fi
echo '<svg xmlns="http://www.w3.org/2000/svg" width="40" height="30"><rect width="40" height="30" fill="red"/></svg>' > bird.svg
browser_check --screenshot bird.svg clipped.png 40x30
browser_check --screenshot bird.svg clipped.png 40x30 5,5,12,10
python3 -c 'import struct; assert struct.unpack(">II", open("clipped.png","rb").read(24)[16:24]) == (12,10)'
google-chrome --headless --no-sandbox --disable-gpu --disable-dev-shm-usage --screenshot="$PWD/raw.png" "file://$PWD/bird.svg" 2>chrome.log || { cat chrome.log; exit 1; }
test -s raw.png
echo checked-negative-exit`
	result, err := tool.ExecuteWithPermission(t.Context(), map[string]any{"command": command}, permReq())
	if err != nil || !result.Process.ExitCodeKnown || result.Process.ExitCode != 0 {
		t.Fatalf("browser check: %+v, %v", result, err)
	}
	for _, want := range []string{"checked-negative-exit", "Browser check passed (2 steps)", "Browser check failed: Step 1: Error: broken interaction", "Browser check failed: Page failed to load: HTTP 404"} {
		if !strings.Contains(result.Text, want) {
			t.Fatalf("missing %q in %s", want, result.Text)
		}
	}
}
