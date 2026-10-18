package recording

import (
	"os"
	"path/filepath"
)

func lockRun(dir string) (*os.File, bool, error) {
	f, err := openRunLock(filepath.Join(dir, ".active.lock"))
	if err != nil {
		return nil, false, err
	}
	ok, err := tryRunLock(f)
	if err != nil || !ok {
		_ = f.Close()
		return nil, ok, err
	}
	return f, true, nil
}
