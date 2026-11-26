package session

import (
	"encoding/json"
	"errors"
	"os"
)

const commitHeaderName = "session.commit.json"

// The small header fences recovery writes if session.json itself becomes
// unavailable. Publish and sync it first, so a missing manifest can never
// authorize a writer whose generation was superseded. A readable manifest
// remains authoritative after an interrupted commit.
func commitManifest(root *os.Root, previous snapshotHeader, manifest []byte) (bool, error) {
	var next snapshotHeader
	if err := json.Unmarshal(manifest, &next); err != nil {
		return false, err
	}
	// Legacy manifests acquire a revision on their first content save. A
	// standalone rename must preserve their on-disk format.
	if next.Revision == "" {
		return replaceRootFile(root, "session.json", manifest, 0600)
	}
	header, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	published, err := replaceRootFile(root, commitHeaderName, header, 0600)
	if !published {
		return false, err
	}
	if err == nil {
		var committed bool
		committed, err = replaceRootFile(root, "session.json", manifest, 0600)
		if committed {
			return true, err
		}
	}
	// The caller still owns the previous generation. Restore its credential
	// before releasing the lock, including when a first commit is retried.
	if previous.Revision == "" {
		return false, errors.Join(err, root.Remove(commitHeaderName))
	}
	header, restoreErr := json.Marshal(previous)
	if restoreErr == nil {
		_, restoreErr = replaceRootFile(root, commitHeaderName, header, 0600)
	}
	return false, errors.Join(err, restoreErr)
}
