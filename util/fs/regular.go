package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrNotRegular = errors.New("path is not a regular file")
var ErrFileTooLarge = errors.New("file exceeds size limit")

// OpenRegularFile rejects special nodes using the opened handle's metadata.
// Unix opens are nonblocking so a FIFO replacement cannot trap the open itself.
func OpenRegularFile(path string) (*os.File, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	return checkRegularFile(file)
}

func checkRegularFile(file *os.File) (*os.File, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, file.Name())
	}
	return file, nil
}

// ReadRegularFile bounds both existing files and files that grow while read.
func ReadRegularFile(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	return readRegularFile(ctx, path, maxBytes, OpenRegularFile)
}

// ReadRegularFileInRoot bounds reads and prevents symlink traversal outside root.
func ReadRegularFileInRoot(ctx context.Context, rootPath, path string, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readRegularFile(ctx, path, maxBytes, func(name string) (*os.File, error) {
		file, err := openRegularFileInRoot(root, name)
		if err != nil {
			return nil, err
		}
		return checkRegularFile(file)
	})
}

func readRegularFile(ctx context.Context, path string, maxBytes int64, open func(string) (*os.File, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes < 0 || maxBytes == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("invalid file size limit")
	}
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %s", ErrFileTooLarge, path)
	}
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stop()
	data, err := io.ReadAll(io.LimitReader(regularReader{ctx: ctx, file: file}, maxBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %s", ErrFileTooLarge, path)
	}
	return data, nil
}

type regularReader struct {
	ctx  context.Context
	file *os.File
}

func (r regularReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Read(data)
}
