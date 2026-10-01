//go:build windows

package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openImageFile(path string) (*os.File, error) {
	expected, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(expected), "/", `\`))
	if strings.HasPrefix(lower, `\\`) || strings.HasPrefix(lower, `\??\`) {
		return nil, fmt.Errorf("image path must be on a local filesystem")
	}
	volume := filepath.VolumeName(expected)
	if len(volume) != 2 || volume[1] != ':' {
		return nil, fmt.Errorf("image path must use a local drive")
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return nil, err
	}
	if windows.GetDriveType(root) == windows.DRIVE_REMOTE {
		return nil, fmt.Errorf("image path must not use a mapped network drive")
	}
	objectName, err := windows.NewNTUnicodeString(`\??\` + expected)
	if err != nil {
		return nil, err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{
		ObjectName: objectName,
		Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	var allocationSize int64
	err = windows.NtCreateFile(
		&handle, windows.FILE_GENERIC_READ|windows.SYNCHRONIZE, attributes, &status,
		&allocationSize, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT,
		0, 0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), expected)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("open image file")
	}
	actual, err := finalWindowsPath(windows.Handle(file.Fd()))
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !strings.EqualFold(filepath.Clean(expected), filepath.Clean(actual)) {
		_ = file.Close()
		return nil, fmt.Errorf("image path changed during secure open")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("image path is not a regular file")
	}
	return file, nil
}

func finalWindowsPath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buffer)) {
			path := string(utf16.Decode(buffer[:n]))
			if strings.HasPrefix(path, `\\?\UNC\`) {
				return `\\` + strings.TrimPrefix(path, `\\?\UNC\`), nil
			}
			return strings.TrimPrefix(path, `\\?\`), nil
		}
		buffer = make([]uint16, n+1)
	}
}
