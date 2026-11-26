package recording

import (
	"os"
	"path/filepath"

	"nekocode/util/filelock"
)

func lockRun(dir string) (*os.File, bool, error) {
	f, err := openRunLock(filepath.Join(dir, ".active.lock"))
	if err != nil {
		return nil, false, err
	}
	ok, err := filelock.TryLock(f)
	if err != nil || !ok {
		_ = f.Close()
		return nil, ok, err
	}
	return f, true, nil
}
