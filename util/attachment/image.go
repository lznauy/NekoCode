package attachment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nekocode/util/fs"
)

const MaxImageBytes = 20 << 20

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ImageDir(sessionID string) (string, error) {
	if !safeSessionID.MatchString(sessionID) {
		return "", fmt.Errorf("invalid attachment session id")
	}
	return filepath.Join(fs.NekocodeHome(), "tmp", "images", sessionID), nil
}

func SaveImage(sessionID string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("clipboard image is empty")
	}
	if len(data) > MaxImageBytes {
		return "", fmt.Errorf("clipboard image exceeds 20 MiB limit")
	}
	ext, err := imageExtension(data)
	if err != nil {
		return "", err
	}
	dir, err := ImageDir(sessionID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create attachment directory: %w", err)
	}
	root := filepath.Join(fs.NekocodeHome(), "tmp", "images")
	if allowed, err := pathWithinResolvedRoot(root, dir); err != nil || !allowed {
		if err != nil {
			return "", fmt.Errorf("resolve attachment directory: %w", err)
		}
		return "", fmt.Errorf("attachment session directory escapes temporary image storage")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure attachment directory: %w", err)
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("name attachment: %w", err)
	}
	path := filepath.Join(dir, "paste-"+hex.EncodeToString(random)+ext)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("save clipboard image: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("save clipboard image: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("save clipboard image: %w", err)
	}
	return path, nil
}

func DeleteImage(path string) error {
	root := filepath.Join(fs.NekocodeHome(), "tmp", "images")
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || len(rel) < 3 || rel[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("attachment path is outside temporary image storage")
	}
	if _, err := os.Lstat(abs); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	allowed, err := pathWithinResolvedRoot(root, filepath.Dir(abs))
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("attachment path resolves outside temporary image storage")
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func pathWithinResolvedRoot(root, path string) (bool, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func DeleteSession(sessionID string) error {
	dir, err := ImageDir(sessionID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ReconcileSessionImages recovers staged attachment transactions and
// removes ordinary image directories whose owning session was never persisted.
// If session state cannot be determined, attachments are preserved fail-closed.
func ReconcileSessionImages(sessionExists func(string) (bool, error)) error {
	root := filepath.Join(fs.NekocodeHome(), "tmp", "images")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list session image storage: %w", err)
	}
	var cleanupErrors []error
	for _, entry := range entries {
		sessionID, ok := stagedSessionID(entry.Name())
		if !ok {
			if !entry.IsDir() || !safeSessionID.MatchString(entry.Name()) || sessionExists == nil {
				continue
			}
			exists, checkErr := sessionExists(entry.Name())
			if checkErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("check attachment session %s: %w", entry.Name(), checkErr))
				continue
			}
			if exists {
				continue
			}
			dir, dirErr := ImageDir(entry.Name())
			if dirErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("resolve orphaned attachment session %s: %w", entry.Name(), dirErr))
				continue
			}
			if removeErr := os.RemoveAll(dir); removeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove orphaned attachment session %s: %w", entry.Name(), removeErr))
			}
			continue
		}
		staged := filepath.Join(root, entry.Name())
		exists := false
		if sessionExists != nil {
			var checkErr error
			exists, checkErr = sessionExists(sessionID)
			if checkErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("check pending attachment delete %s: %w", entry.Name(), checkErr))
				continue
			}
		}
		if exists {
			dir, err := ImageDir(sessionID)
			if err == nil {
				err = os.Rename(staged, dir)
			}
			if err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("restore pending attachment delete %s: %w", entry.Name(), err))
			}
			continue
		}
		if err := os.RemoveAll(staged); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove pending attachment delete %s: %w", entry.Name(), err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func stagedSessionID(name string) (string, bool) {
	const prefix = ".deleting-"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name, prefix)
	separator := strings.LastIndexByte(rest, '-')
	if separator <= 0 || len(rest)-separator-1 != 16 {
		return "", false
	}
	if _, err := hex.DecodeString(rest[separator+1:]); err != nil {
		return "", false
	}
	sessionID := rest[:separator]
	return sessionID, safeSessionID.MatchString(sessionID)
}

// SessionDelete stages a session attachment directory with an atomic rename,
// allowing callers to restore it if deleting the owning session record fails.
type SessionDelete struct {
	dir    string
	staged string
}

func BeginSessionDelete(sessionID string) (*SessionDelete, error) {
	dir, err := ImageDir(sessionID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return &SessionDelete{dir: dir}, nil
	} else if err != nil {
		return nil, err
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("stage session attachments: %w", err)
	}
	staged := filepath.Join(filepath.Dir(dir), ".deleting-"+sessionID+"-"+hex.EncodeToString(random))
	if err := os.Rename(dir, staged); err != nil {
		return nil, fmt.Errorf("stage session attachments: %w", err)
	}
	return &SessionDelete{dir: dir, staged: staged}, nil
}

func (d *SessionDelete) Rollback() error {
	if d == nil || d.staged == "" {
		return nil
	}
	if err := os.Rename(d.staged, d.dir); err != nil {
		return fmt.Errorf("restore session attachments: %w", err)
	}
	d.staged = ""
	return nil
}

func (d *SessionDelete) Commit() error {
	if d == nil || d.staged == "" {
		return nil
	}
	if err := os.RemoveAll(d.staged); err != nil {
		return fmt.Errorf("remove staged session attachments: %w", err)
	}
	d.staged = ""
	return nil
}

func imageExtension(data []byte) (string, error) {
	mime := http.DetectContentType(data)
	if mime == "application/octet-stream" && len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		mime = "image/webp"
	}
	switch mime {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", fmt.Errorf("unsupported clipboard image type %q", mime)
	}
}
