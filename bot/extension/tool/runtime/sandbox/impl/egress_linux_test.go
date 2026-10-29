//go:build linux

package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestrictedNativeIsolation(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("requires Linux native namespaces")
	}
	ws := t.TempDir()
	for _, domains := range [][]string{{}, {"example.com"}} {
		profile := Profile{Workspace: ws, Network: true, OutboundDomains: domains}
		out, err := Run(t.Context(), "echo restricted-ok", profile, 10*time.Second)
		if err != nil || !strings.Contains(out, "restricted-ok") {
			t.Fatalf("%s %v", out, err)
		}
		out, err = Run(t.Context(), "curl -sS --max-time 2 --noproxy '*' http://127.0.0.1:9 2>&1; echo direct-exit:$?", profile, 10*time.Second)
		if err != nil || strings.Contains(out, "direct-exit:0") {
			t.Fatalf("direct bypass: %s %v", out, err)
		}
	}
	profile := Profile{Workspace: ws, OutboundDomains: []string{"example.com"}}
	out, err := Run(t.Context(), "curl -sS --max-time 3 http://denied.example", profile, 10*time.Second)
	if err != nil || !strings.Contains(out, "egress") {
		t.Fatalf("denial missing: %s %v", out, err)
	}
	// A host Unix socket remains mounted through the workspace, but seccomp
	// must prevent both the command and descendants from connecting to it.
	socket := filepath.Join(ws, "host.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	out, err = Run(t.Context(), "sh -c 'curl -sS --max-time 2 --unix-socket "+socket+" http://localhost' 2>&1; echo unix-exit:$?", profile, 10*time.Second)
	if err != nil || strings.Contains(out, "unix-exit:0") {
		t.Fatalf("unix bypass: %s %v", out, err)
	}
}
func TestRestrictedNativeHTTPAndAsyncCleanup(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("requires Linux native namespaces")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "fixture-allowed") }))
	defer upstream.Close()
	old := createEgressProxy
	defer func() { createEgressProxy = old }()
	var sockets []string
	createEgressProxy = func(domains []string) (*egressProxy, error) {
		p, err := startEgressProxy(domains)
		if err != nil {
			return nil, err
		}
		sockets = append(sockets, p.socket())
		p.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialEgress(ctx, address, domains, func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}, func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
			})
		}
		return p, nil
	}
	profile := Profile{Workspace: t.TempDir(), OutboundDomains: []string{"example.com"}}
	out, err := Run(t.Context(), "curl -sS --max-time 3 http://example.com/", profile, 10*time.Second)
	if err != nil || !strings.Contains(out, "fixture-allowed") {
		t.Fatalf("HTTP bridge: %s %v", out, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	p, err := Start(ctx, "sleep 60", profile)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel left command running")
	}
	for _, socket := range sockets {
		if _, err := os.Stat(socket); !os.IsNotExist(err) {
			t.Errorf("socket leaked: %s %v", socket, err)
		}
	}
}
func TestRestrictedNativeKeepsNetworkNamespace(t *testing.T) {
	launch, err := prepareNativeLaunch("true", Profile{Workspace: t.TempDir(), Network: true, OutboundDomains: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer launch.cleanup()
	var profile Profile
	if err := json.Unmarshal([]byte(launch.args[1]), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.OutboundDomains == nil || launch.sysproc.Cloneflags&0x40000000 == 0 {
		t.Fatal("restricted policy lost its isolated namespace")
	}
}

func TestRestrictedNativeExitCode(t *testing.T) {
	if !isNativeAvailable() {
		t.Skip("requires Linux native namespaces")
	}
	p, err := Start(t.Context(), "exit 7", Profile{Workspace: t.TempDir(), OutboundDomains: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	// Drain before waiting, matching the Process contract.
	go io.Copy(io.Discard, p.Stdout())
	go io.Copy(io.Discard, p.Stderr())
	err = p.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("restricted exit status changed: %v", err)
	}
}

func TestRestrictedPolicyNeverFallsBack(t *testing.T) {
	old := isNativeAvailable
	isNativeAvailable = func() bool { return false }
	defer func() { isNativeAvailable = old }()
	profile := Profile{Workspace: t.TempDir(), OutboundDomains: []string{}}
	command := "touch should-not-run"
	_, err := Run(t.Context(), command, profile, time.Second)
	var isolated NetworkIsolationError
	if !errors.As(err, &isolated) {
		t.Fatalf("wrong fallback error: %v", err)
	}
	_, err = Start(t.Context(), command, profile)
	if !errors.As(err, &isolated) {
		t.Fatalf("wrong async fallback error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profile.Workspace, "should-not-run")); !os.IsNotExist(err) {
		t.Fatal("restricted command ran without namespaces")
	}
}
