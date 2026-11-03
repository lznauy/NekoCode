package impl

import (
	"context"
	"errors"
	"nekocode/bot/extension/tool/runtime/authority"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSandboxAuthorityProtectionPrecedesBackendFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, err := authority.Root()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "authority")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	for _, profile := range []Profile{
		{Workspace: home},
		{Workspace: t.TempDir(), WritePaths: []string{root}},
		{Workspace: t.TempDir(), WritePaths: []string{alias}},
		{Workspace: t.TempDir(), WritePaths: []string{"~/.nekocode/permissions/new"}},
	} {
		_, err := Run(context.Background(), "true", profile, time.Second)
		var unavailable UnavailableError
		if err == nil || errors.As(err, &unavailable) {
			t.Fatalf("Run must fail closed before backend fallback: %v", err)
		}
		_, err = Start(context.Background(), "true", profile)
		if err == nil || errors.As(err, &unavailable) {
			t.Fatalf("Start must fail closed before backend fallback: %v", err)
		}
	}
	if err := protectAuthority(Profile{Workspace: home, Mode: ModeReadOnly}); err != nil {
		t.Fatalf("read-only ancestor rejected: %v", err)
	}
}
