package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPermanentRootApprovalSurvivesSessionSwitch(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		m := New(t.TempDir(), nil)
		extra := t.TempDir()
		approval, err := m.PrepareRootApproval(extra, AccessReadWrite)
		if err != nil {
			t.Fatal(err)
		}
		var persist func(Root) error
		if permanent {
			persist = func(Root) error { return nil }
		}
		if _, err := m.CommitRootApproval(approval, persist); err != nil {
			t.Fatal(err)
		}
		m.SetSession("next")
		if _, _, allowed, err := m.CheckWrite(filepath.Join(extra, "file")); err != nil || allowed != permanent {
			t.Fatalf("permanent=%t allowed=%t err=%v", permanent, allowed, err)
		}
	}
}

func TestCanonicalRootDoesNotRebindDuringInstallation(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	approved, other := filepath.Join(base, "approved"), filepath.Join(base, "other")
	if err := os.Mkdir(approved, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	root := Root{Path: approved, Access: AccessReadWrite, Canonical: true}
	if err := os.Rename(approved, approved+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, approved); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	m := New(t.TempDir(), []Root{root})
	if _, _, allowed, err := m.CheckWrite(filepath.Join(other, "file")); err != nil || allowed {
		t.Fatalf("canonical grant rebound: %t %v", allowed, err)
	}
	root.Canonical = false
	m.Configure(m.Primary(), []Root{root})
	if _, _, allowed, err := m.CheckWrite(filepath.Join(other, "file")); err != nil || !allowed {
		t.Fatalf("explicit configured link lost: %t %v", allowed, err)
	}
}

func TestRootApprovalPersistenceFailureDoesNotGrantAuthority(t *testing.T) {
	m := New(t.TempDir(), nil)
	extra := t.TempDir()
	a, err := m.PrepareRootApproval(extra, AccessReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("disk full")
	if _, err := m.CommitRootApproval(a, func(Root) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("persistence error lost: %v", err)
	}
	if _, _, allowed, err := m.CheckWrite(filepath.Join(extra, "file.txt")); err != nil || allowed {
		t.Fatalf("failed persistence published authority: allowed=%t err=%v", allowed, err)
	}
	if _, err := m.CommitRootApproval(a, nil); err != nil {
		t.Fatalf("one-time approval should remain usable: %v", err)
	}
}

func TestRootApprovalCannotBeCommittedByAnotherManager(t *testing.T) {
	a, b := New(t.TempDir(), nil), New(t.TempDir(), nil)
	approval, err := a.PrepareRootApproval(t.TempDir(), AccessReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	persisted := false
	if _, err := b.CommitRootApproval(approval, func(Root) error { persisted = true; return nil }); err == nil || persisted {
		t.Fatalf("foreign approval accepted or persisted: %v", err)
	}
}

func TestParallelRootApprovalsInSameSessionRemainUsable(t *testing.T) {
	m := New(t.TempDir(), nil)
	a, b := t.TempDir(), t.TempDir()
	first, err := m.PrepareRootApproval(a, AccessReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.PrepareRootApproval(b, AccessReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, approval := range []RootApproval{first, second} {
		wg.Add(1)
		go func(approval RootApproval) {
			defer wg.Done()
			_, err := m.CommitRootApproval(approval, nil)
			errs <- err
		}(approval)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{a, b} {
		if _, _, allowed, err := m.CheckWrite(filepath.Join(root, "file.txt")); err != nil || !allowed {
			t.Fatalf("parallel approval missing: allowed=%t err=%v", allowed, err)
		}
	}
}
