package subagent

import (
	"strings"

	"nekocode/bot/tools"
)

func isSensitiveCall(c tools.ToolCallItem) bool {
	switch c.Name {
	case "bash":
		cmd, _ := c.Args["command"].(string)
		return isDangerousCommand(cmd)
	case "read", "write", "edit":
		paths := extractPaths(c)
		for _, p := range paths {
			if isSensitivePath(p) {
				return true
			}
		}
	case "grep":
		pattern, _ := c.Args["pattern"].(string)
		if isSensitivePath(pattern) {
			return true
		}
		fallthrough
	case "glob":
		p, _ := c.Args["path"].(string)
		if isSensitivePath(p) {
			return true
		}
	}
	return false
}

func extractPaths(c tools.ToolCallItem) []string {
	if p, ok := c.Args["path"].(string); ok && p != "" {
		return []string{p}
	}
	return nil
}

func isSensitivePath(p string) bool {
	lower := strings.ToLower(p)
	for _, f := range []string{
		".env", ".env.local", ".env.production",
		"credentials", "secrets", "password",
		".git/config", ".gitconfig",
		"id_rsa", "id_ed25519", "private key",
		".claude/settings.json", ".claude/settings.local.json",
		"/etc/shadow", "/etc/passwd",
	} {
		if strings.Contains(lower, f) {
			return true
		}
	}
	return false
}

func isDangerousCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, pat := range []string{
		"rm -rf", "rm -r", "rmdir",
		"git push --force", "git push -f",
		"git reset --hard",
		"chmod 777", "chmod -r 777",
		"> /dev/", "dd if=",
		"mkfs.", "format ",
		":(){ :|:& };:",
		"curl", "wget",
	} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
