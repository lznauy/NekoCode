package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func useWindowsClipboard() bool {
	if runtime.GOOS != "linux" || os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.Contains(strings.ToLower(string(release)), "microsoft")
}

// Static scripts run in Windows PowerShell's STA thread. Binary stdout avoids
// console encodings and base64 expansion; nothing is written to a shared file.
const windowsClipboardImageScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$image = [System.Windows.Forms.Clipboard]::GetImage()
if ($null -eq $image) { exit 3 }
$stream = New-Object System.IO.MemoryStream
try {
    $image.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
    if ($stream.Length -gt 20971520) { throw 'Clipboard image exceeds 20 MiB limit' }
    $stream.Position = 0
    $stream.CopyTo([Console]::OpenStandardOutput())
} finally {
    $stream.Dispose()
    $image.Dispose()
}`

const windowsClipboardTextScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$text = [System.Windows.Forms.Clipboard]::GetText()
if ([string]::IsNullOrEmpty($text)) { exit 3 }
$bytes = [System.Text.Encoding]::UTF8.GetBytes($text)
if ($bytes.Length -gt 1048576) { throw 'Clipboard text exceeds 1 MiB limit' }
[Console]::OpenStandardOutput().Write($bytes, 0, $bytes.Length)
`

func readWindowsClipboard(ctx context.Context, format string, limit int64) ([]byte, error) {
	script := windowsClipboardImageScript
	if format == clipboardHelperText {
		script = windowsClipboardTextScript
	}
	path, err := exec.LookPath("powershell.exe")
	if err != nil {
		return nil, fmt.Errorf("WSL 无法调用 Windows 剪贴板：未找到 powershell.exe，请检查 WSL Windows 互操作与 PATH；安装 Xvfb 无法解决此问题")
	}
	cmd := exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	data, err := readClipboardCommand(ctx, cmd, format, limit)
	if err != nil && err != errClipboardNoData {
		return nil, fmt.Errorf("读取 Windows 剪贴板失败，请检查 WSL Windows 互操作：%w", err)
	}
	return data, err
}
