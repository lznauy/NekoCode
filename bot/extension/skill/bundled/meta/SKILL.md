---
name: skill-creator
description: Create, review, or update SKILL.md files. Use when the user wants to write a new skill, improve an existing one, or audit a skill for issues.
when_to_use: User says "write a skill", "create a skill", "review this skill", "check my SKILL.md", "audit my skill", or asks how to write effective skills.
---

# Skill Creator

## Mode Routing

| User says | Mode |
|-----------|------|
| "create / write / new skill" | Create |
| "review / check / audit my SKILL.md" | Review |
| "fix / update / improve this skill" | Update |

If unclear, ask: "Creating a new skill, or reviewing an existing one?"

---

## Create Mode

### Step 1: Gather

Ask one round, not more:

- What does the skill do? (one sentence)
- What tools does it need? (bash, read, write, edit, grep, glob, etc.)
- Fork or inline? (fork for 5+ steps, heavy processing, or token-intensive work)
- Output location? Default: `.nekocode/skills/<name>/SKILL.md` in current project.

### Step 2: Draft

Write the SKILL.md with this structure:

```yaml
---
name: <kebab-case>
description: <one-line summary with trigger keywords>
when_to_use: <when the model should auto-invoke>
allowed-tools: [bash, read, write]  # if fork mode
context: inline|fork
agent: executor                      # if fork mode
max_steps: N                         # if fork mode, default 4
context_window: N                      # if fork mode, default 16000
---

# <Title>

## Steps
1. First concrete action
2. Second concrete action
...

## Output
<what the model produces, with format example>
```

### Step 3: Validate

Check against the anti-patterns table below before writing. Fix any issues found.

### Step 4: Write

Write the file to the specified location.

---

## Review Mode

### Step 1: Read

Read the target SKILL.md file.

### Step 2: Audit

Check each item:

- [ ] `name` is kebab-case, short, descriptive
- [ ] `description` contains trigger keywords users actually type
- [ ] `when_to_use` gives clear auto-invoke guidance
- [ ] Body has numbered steps, not abstract advice
- [ ] Steps are concrete: reads specific files, runs specific commands
- [ ] Output format is specified with examples
- [ ] File references use relative paths (skill dir is the base)
- [ ] Under 5K characters (reference material goes in auxiliary files)
- [ ] One task per skill (no mixed concerns)
- [ ] If fork mode: `allowed-tools`, `max_steps`, `context_window` are set

### Step 3: Report

```
## Skill Review: `<name>`

### Issues Found

- [severity] <path>:<line if known> — <issue>
  Fix: <exact edit to apply>

### Summary
N issues: N critical, N warning, N info
```

Severity levels:
- **[!]** — missing required fields, broken references, unsafe allowed-tools
- **[~]** — vague steps, missing when_to_use, description too generic
- **[-]** — style issues, could be more concise, missing examples

---

## Update Mode

1. Read the current SKILL.md
2. Ask what to change (one question)
3. Apply the change via edit
4. Re-run review checklist on the changed sections

---

## Best Practices

### Concrete > Abstract

Every step should name specific files, commands, or patterns. The model follows instructions literally — "handle errors" becomes "log and return 500", never actual error handling. Write exactly what you want.

```markdown
# Bad
1. Understand the codebase structure
2. Write appropriate tests

# Good
1. Read `go.mod` to confirm module name
2. Read the target file to find exported functions
3. For each exported func, write a table-driven test in `<file>_test.go`
```

### Short > Complete

Reference material goes in auxiliary files. SKILL.md should be a workflow, not an encyclopedia. If a section exceeds 20 lines, move it to a separate file and reference it.

### Trigger Words > Generic Names

The `description` is scanned by the model to decide auto-invocation. Include the exact words users say:

```yaml
# Bad
description: Helps with deployment

# Good
description: Deploy the project to production. Use when the user says deploy, release, ship, go live, or publish.
```

### Fork for Heavy Work

Use `context: fork` when the skill:
- Has 5+ sequential steps
- Processes or generates many files
- Needs to read extensive reference material
- Would fill the main context window

---

## Anti-patterns

| Issue | Example | Fix |
|-------|---------|-----|
| Too verbose | 15K of instructions inline | Move reference content to auxiliary files; keep SKILL.md under 5K |
| Vague steps | "Handle errors appropriately" | "Log the error to stderr and return exit code 1" |
| No trigger words | `description: Helps with stuff` | Include exact phrases users type |
| Hardcoded paths | `/home/user/templates/` | Use relative paths from skill directory |
| Mixed concerns | deploy + test + lint in one file | Split into separate skills |
| abstract goal | "Ensure code quality" (unenforceable) | "Run `go vet ./...` and fix all warnings" |

---

## Gotchas

| What happened | Rule |
|---------------|------|
| Skill created but model never invokes it | `description` has no trigger keywords the user actually types |
| Model follows steps literally but output is wrong | Steps are too abstract; add exact commands and output examples |
| Fork mode skill hangs or times out | `max_steps` too low or `context_window` too small for the workload |
| Auxiliary files not found | Model doesn't know skill dir; add explicit `read path/to/file` step |
| Skill contradicts system prompt | System prompt rules always win; don't try to override core behavior |
| Template written to skill dir instead of cwd | Say "write to `<name>` in the current directory", not "write to `<name>`" |
