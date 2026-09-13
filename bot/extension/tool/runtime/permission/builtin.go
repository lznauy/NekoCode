package permission

// Builtin rules: the default policy baked into NekoCode. User/project rules
// are evaluated alongside these (deny from any source wins; user/project can
// add ask/allow to override the builtin defaults for unmatched calls).
//
// Shell rules use command-prefix matching (claude-code style): a specifier
// like "npm run *" matches commands starting with "npm run ". The bash
// matcher handles the wildcard logic; here we only declare the policies.
//
// Shell uses explicit authorization: builtins block obviously unsafe commands,
// force prompts for destructive ones, and allow common read-only inspection
// commands. Everything else falls to the engine caller's default ask until a
// user/project/remembered allow rule exists.

var builtinRules = []Rule{
	// --- Shell: hard-deny (irreversible / privilege escalation) ---
	{Tool: "shell", Specifier: "sudo *", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "sudo", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "eval *", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "dd *", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "mkfs*", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "ssh *", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "| bash", Effect: EffectDeny, Source: "builtin"},
	{Tool: "shell", Specifier: "| sh", Effect: EffectDeny, Source: "builtin"},

	// --- Shell: ask (destructive but reversible within workspace) ---
	{Tool: "shell", Specifier: "rm *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "rmdir *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "kill *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "pkill *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "chmod *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "chown *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "git push *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "git push", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "git reset --hard *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "mv *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "shutdown *", Effect: EffectAsk, Source: "builtin"},
	{Tool: "shell", Specifier: "reboot *", Effect: EffectAsk, Source: "builtin"},

	// --- Shell: allow common read-only inspection ---
	{Tool: "shell", Specifier: "ls *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "pwd", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "whoami", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "date", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "printenv *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "which *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "uname *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "hostname", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "wc *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "cat *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "head *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "tail *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "less *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "more *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "du *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "df *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "free *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "uptime", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "pgrep *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "file *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "stat *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "go version", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "go env *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "go doc *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "go vet *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "go fmt *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git status *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git log *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git diff *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git show *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git blame *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git tag *", Effect: EffectAllow, Source: "builtin"},
	{Tool: "shell", Specifier: "git remote *", Effect: EffectAllow, Source: "builtin"},

	// --- File tools: workspace writes default to allow (path boundary enforces scope) ---
	{Tool: "write", Effect: EffectAllow, Source: "builtin"},
	{Tool: "edit", Effect: EffectAllow, Source: "builtin"},
	{Tool: "read", Effect: EffectAllow, Source: "builtin"},
	{Tool: "list", Effect: EffectAllow, Source: "builtin"},
	{Tool: "tree", Effect: EffectAllow, Source: "builtin"},
	{Tool: "glob", Effect: EffectAllow, Source: "builtin"},
	{Tool: "grep", Effect: EffectAllow, Source: "builtin"},
	{Tool: "diff", Effect: EffectAllow, Source: "builtin"},

	// --- Other tools: default allow (stateless / non-destructive) ---
	{Tool: "todo_write", Effect: EffectAllow, Source: "builtin"},
	{Tool: "task", Effect: EffectAllow, Source: "builtin"},
	{Tool: "web_search", Effect: EffectAllow, Source: "builtin"},
	{Tool: "web_fetch", Effect: EffectAllow, Source: "builtin"},
	{Tool: "web_extract", Effect: EffectAllow, Source: "builtin"},
	{Tool: "question", Effect: EffectAllow, Source: "builtin"},
}

// BuiltinRules returns a copy of the builtin rule set.
func BuiltinRules() []Rule {
	out := make([]Rule, len(builtinRules))
	copy(out, builtinRules)
	return out
}

var builtinSandboxRules = []SandboxRule{
	{Rule: Rule{Tool: "shell", Specifier: "npm install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "npm ci *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "npm update *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "npm create *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "npm exec *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "npx *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pnpm install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pnpm add *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pnpm update *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pnpm create *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pnpm dlx *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pnpm dev *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "yarn install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "yarn add *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "yarn up *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "yarn create *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "yarn dlx *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "bun install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "bun add *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "bun update *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "bun create *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "bunx *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pip install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "pip3 install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "python -m pip install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "python3 -m pip install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "go get *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "go install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "go mod download *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "cargo install *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "cargo add *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "cargo fetch *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "git clone *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "git fetch *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "git pull *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "curl *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "wget *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "docker pull *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
	{Rule: Rule{Tool: "shell", Specifier: "degit *", Source: "builtin"}, Profile: SandboxProfile{Network: true}},
}

func BuiltinSandboxRules() []SandboxRule {
	out := make([]SandboxRule, len(builtinSandboxRules))
	copy(out, builtinSandboxRules)
	return out
}
