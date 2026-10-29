package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Every live Manager retains a shared lease. Rebuild holds an exclusive lease
// from before moving the database until the replacement has been initialized.
// The lock file is permanent: removing it would split the lock's identity.
type indexLease struct {
	file *os.File
	gate *os.File
}

func acquireIndexLease(root string, exclusive bool) (*indexLease, error) {
	dir := filepath.Join(root, ".nekocode")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	gate, err := os.OpenFile(filepath.Join(dir, "index-rebuild.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	gateOK, err := tryIndexLock(gate, exclusive)
	if err != nil || !gateOK {
		_ = gate.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("project index maintenance is in use by another instance")
	}
	keepGate := false
	defer func() {
		if !keepGate {
			_ = gate.Close()
		}
	}()
	file, err := os.OpenFile(filepath.Join(dir, "index.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ok, err := tryIndexLock(file, exclusive)
	if err != nil || !ok {
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("lock project index: %w", err)
		}
		return nil, fmt.Errorf("project index is in use by another instance; close its index or exit that instance before rebuilding")
	}
	keepGate = exclusive
	lease := &indexLease{file: file}
	if exclusive {
		lease.gate = gate
	}
	return lease, nil
}

func (l *indexLease) share() error {
	ok, err := shareIndexLock(l.file)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("cannot share project index lease")
	}
	if l.gate != nil {
		_ = l.gate.Close()
		l.gate = nil
	}
	return nil
}
func (l *indexLease) close() error {
	if l == nil {
		return nil
	}
	var err error
	if l.file != nil {
		err = l.file.Close()
	}
	if l.gate != nil {
		err = errors.Join(err, l.gate.Close())
	}
	return err
}
