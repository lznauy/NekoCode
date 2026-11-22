package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAllowOutputFileThroughDirectoryAlias(t *testing.T) {
	m := New(t.TempDir(), nil)
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "outputs")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	file := filepath.Join(dir, "result")
	if err := os.WriteFile(file, []byte("output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.AllowOutputFile(filepath.Join(alias, "result")); err != nil {
		t.Fatal(err)
	}
	if _, grant, ok, err := m.CheckRead(file); err != nil || !ok || !grant.FileOnly || grant.Access != AccessReadOnly {
		t.Fatalf("unexpected output grant %+v, allowed=%t, err=%v", grant, ok, err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, dir, filepath.Join(dir, "missing")} {
		if err := m.AllowOutputFile(path); err == nil {
			t.Errorf("accepted non-regular output path %q", path)
		}
	}
}

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

func TestFileGrantMatchesOnlyTheApprovedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	path := filepath.Join(dir, "item ")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	grant, err := CandidateGrant(path, AccessReadWrite)
	if err != nil || !grant.FileOnly {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	m := New(t.TempDir(), nil)
	approval, err := m.PrepareGrant(grant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CommitRootApproval(approval, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := m.CheckRead(path); !ok || err != nil {
		t.Fatalf("file: %v %v", ok, err)
	}
	if _, _, allowed, err := m.CheckWrite(filepath.Join(dir, "item")); err != nil || allowed {
		t.Fatalf("granted a different filename: allowed=%t err=%v", allowed, err)
	}
	if err = os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, filepath.Join(path, "child")} {
		if _, _, ok, err := m.CheckWrite(candidate); ok || err != nil {
			t.Fatalf("directory access for %s: %v %v", candidate, ok, err)
		}
	}
	if _, _, ok, _ := m.CheckReadLexical(filepath.Join(path, "child")); ok {
		t.Fatal("lexical access exceeded file grant")
	}
}
