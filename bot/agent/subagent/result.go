package subagent

import "strings"

type Status int

const (
	StatusCompleted Status = iota
	StatusFailed
	StatusPartial
)

type classification int

const (
	classPass        classification = iota
	classWarn
	classUnavailable
)

type Result struct {
	Status          Status
	Content         string
	TotalTokens     int
	ToolUseCount    int
	DurationMs      int64
	CacheHitTokens  int
	CacheMissTokens int
	classification  classification
}

func FormatResult(r *Result) string {
	if r.classification == classWarn {
		return "SECURITY WARNING: This sub-agent performed actions that may violate security policy.\n\n" + r.Content
	}
	return r.Content
}

func classifyHandoff(rawOutput string) classification {
	lower := strings.ToLower(rawOutput)
	for _, cmd := range []string{
		"rm -rf", "rm -r", "rmdir",
		"git push --force", "git push -f",
		"git reset --hard",
		"chmod 777", "chmod -r 777",
		"> /dev/", "dd if=",
		"mkfs.", "format ",
		":(){ :|:& };:",
	} {
		if strings.Contains(lower, cmd) {
			return classWarn
		}
	}
	for _, f := range []string{
		".env", ".env.local", ".env.production",
		"credentials", "secrets", "password",
		".git/config", ".gitconfig",
		"id_rsa", "id_ed25519", "private key",
		".claude/settings.json", ".claude/settings.local.json",
	} {
		if strings.Contains(lower, f) {
			return classWarn
		}
	}
	return classPass
}
