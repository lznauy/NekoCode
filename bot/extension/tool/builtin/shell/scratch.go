package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type scratchIdentity struct{ owner, actor, directory string }

type scratchDirectory struct {
	path string
	file *os.File
	info os.FileInfo
}

// launchMu serializes setup and shutdown. State is private to one owner and
// working directory; neither HOME nor the host's shared Go cache is writable.
func (r *taskRegistry) prepareScratch(req *startRequest) error {
	directory, err := filepath.EvalSymlinks(req.directory)
	if err != nil {
		return fmt.Errorf("resolve shell workspace: %w", err)
	}
	key := scratchIdentity{req.owner, req.actor, directory}
	state := r.scratch[key]
	if state == nil {
		base, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("locate shell state: %w", err)
		}
		base = filepath.Join(base, "nekocode", "shell")
		if err = os.MkdirAll(base, 0700); err != nil {
			return fmt.Errorf("create shell state: %w", err)
		}
		dir, err := os.MkdirTemp(base, "actor-")
		if err != nil {
			return fmt.Errorf("create actor scratch: %w", err)
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid actor scratch directory: %v", err)
		}
		state = &scratchDirectory{path: dir, info: info}
		if r.scratch == nil {
			r.scratch = make(map[scratchIdentity]*scratchDirectory)
		}
		r.scratch[key] = state
	}
	if err := r.pinScratch(state); err != nil {
		return err
	}
	req.profile.ScratchDir = state.path
	req.profile.ScratchFile = state.file
	return nil
}

// Keep descriptors for live commands pinned. Idle actor caches retain their
// data and inode identity, but need not consume an FD until the toolbox closes.
// The caller holds launchMu, so no launch can enter between sampling and close.
func (r *taskRegistry) pinScratch(state *scratchDirectory) error {
	if state.file != nil {
		return nil
	}
	open := 0
	for _, entry := range r.scratch {
		if entry.file != nil {
			open++
		}
	}
	if open >= maxTasks {
		busy := map[*os.File]bool{}
		r.mu.Lock()
		for _, task := range r.tasks {
			if task.isRunning() {
				busy[task.scratchFile] = true
			}
		}
		r.mu.Unlock()
		for _, entry := range r.scratch {
			if entry.file != nil && !busy[entry.file] {
				if err := entry.file.Close(); err != nil {
					return err
				}
				entry.file = nil
				open--
				break
			}
		}
		if open >= maxTasks {
			return fmt.Errorf("scratch handle limit reached by active commands; wait for one to finish")
		}
	}
	info, err := os.Lstat(state.path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, state.info) {
		return fmt.Errorf("actor scratch path was replaced")
	}
	file, err := os.Open(state.path)
	if err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(state.info, opened) {
		file.Close()
		return fmt.Errorf("actor scratch changed while opening")
	}
	state.file = file
	return nil
}

func (r *taskRegistry) removeScratch() error {
	var failures []error
	for key, state := range r.scratch {
		if state.file != nil {
			if err := state.file.Close(); err != nil {
				failures = append(failures, err)
			}
			state.file = nil
		}
		info, err := os.Lstat(state.path)
		if os.IsNotExist(err) {
			delete(r.scratch, key)
			continue
		}
		if err != nil || !os.SameFile(info, state.info) || info.Mode()&os.ModeSymlink != 0 {
			failures = append(failures, fmt.Errorf("scratch path replaced; refusing cleanup: %s", state.path))
			continue
		}
		if err := os.RemoveAll(state.path); err != nil {
			failures = append(failures, err)
		} else {
			delete(r.scratch, key)
		}
	}
	return errors.Join(failures...)
}
