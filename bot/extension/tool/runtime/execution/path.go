package execution

import "path/filepath"

func normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		parent := filepath.Dir(abs)
		realParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			return abs
		}
		return filepath.Join(realParent, filepath.Base(abs))
	}
	return real
}
