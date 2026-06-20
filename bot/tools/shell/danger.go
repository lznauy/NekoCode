package shell

import (
	"strings"

	"nekocode/common"
)

func classifyShellDanger(cmd string) common.DangerLevel {
	cmdForMatch := stripHeredocBodies(cmd)

	if matchAny(cmdForMatch, []string{
		"sudo", "eval", "nc ", "ncat",
		"telnet", "ssh ", "scp ", "nohup", "disown",
		"> /dev/", "mkfs", "dd ", "chmod 777",
		"| bash", "| sh", "|bash", "|sh",
	}) {
		return common.LevelForbidden
	}

	if matchAny(cmdForMatch, []string{
		"curl", "wget", "rm ", "rmdir", "chmod ", "chown ", "kill", "pkill",
		"shutdown", "reboot", "mv ", "git push", "git reset --hard",
		"git branch -d", "git branch -D",
		"git config --global", "git config --system", "git config --local",
		"git config --replace-all", "git config --unset", "git config --edit",
		"docker rm", "docker rmi",
	}) {
		return common.LevelDestructive
	}

	if matchAny(cmdForMatch, []string{
		"mkdir", "touch ", "cp ", "tar ", "zip ",
		"gzip ", "git commit", "git add", "pip install", "npm install",
		"go install", "cargo install", "make ", "docker build",
	}) {
		return common.LevelWrite
	}

	if hasWriteRedirection(cmdForMatch) {
		return common.LevelWrite
	}
	if isReadOnly(cmd) {
		return common.LevelSafe
	}
	return common.LevelWrite
}

var readOnlyPrefixes = []string{
	"go version", "go env", "go doc", "go vet", "go fmt",
	"git status", "git log", "git diff", "git show",
	"git blame", "git tag", "git remote",
	"pwd", "whoami", "date", "printenv",
	"which", "uname", "hostname", "wc ",
	"cat ", "head ", "tail ", "less ", "more ",
	"du ", "df ", "free ", "uptime", "pgrep",
	"man ", "info ", "file ", "stat ",
}

var readOnlyCommands = []string{
	"ls", "env", "id", "ps", "type",
}

func isReadOnly(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, c := range readOnlyCommands {
		if lower == c || strings.HasPrefix(lower, c+" ") {
			return true
		}
	}
	return false
}

func matchAny(cmd string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}
