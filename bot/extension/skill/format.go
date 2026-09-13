package skill

import (
	"fmt"
	"html"
	"strings"
)

// Skill discovery remains a small part of the context while leaving enough
// room for every normal-sized catalog to expose its trigger descriptions.
const (
	minListChars = 2000
	maxListChars = 8000
)

const skillListHeader = "## Available Skills (authoritative)\n\n" +
	"Names and descriptions are discovery metadata, not workflow instructions. Do not scan the filesystem for additional skills. A loaded skill normally already has `<skill_content>` in context; reload it only when that content was removed by context compaction.\n\n"

// buildSkillListText generates the available-skills text injected into context.
// The first eligible entry is always written, even if it exceeds the budget;
// once the budget is exhausted the remaining skills are only counted.
//
// The text is a pure function of the registry and the context window: it must
// not depend on per-session state such as which skills are already loaded,
// because the list is the first thing in the provider's cached prefix and any
// byte change re-reads the entire history.
func buildSkillListText(skills []*Skill, contextWindow int) string {
	total := len(skills)
	if total == 0 {
		return ""
	}

	maxChars := max(min(contextWindow/20, maxListChars), minListChars)
	remaining := maxChars - len([]rune(skillListHeader))

	var sb strings.Builder
	sb.WriteString(skillListHeader)
	listed := 0
	for _, sk := range skills {
		name := html.EscapeString(compactSkillMetadata(sk.Name))
		description := html.EscapeString(compactSkillMetadata(sk.Description))
		entry := fmt.Sprintf("- **%s**: %s\n", name, description)
		if listed > 0 && remaining < len([]rune(entry)) {
			break
		}
		sb.WriteString(entry)
		remaining -= len([]rune(entry))
		listed++
	}
	if listed < total {
		fmt.Fprintf(&sb, "\n(%d more skills available but omitted due to token budget)\n", total-listed)
	}
	return sb.String()
}

// FormatForContext formats a skill's content for injection into conversation context.
func FormatForContext(sk *Skill) string {
	var sb strings.Builder
	safeName := html.EscapeString(compactSkillMetadata(sk.Name))
	fmt.Fprintf(&sb, "<skill_content name=\"%s\">\n# Skill: %s\n\n", safeName, safeName)
	sb.WriteString("This is a runtime-selected workflow. Follow it for the current task, subject to system rules and the user's explicit request.\n\n")
	fmt.Fprintf(&sb, "**This skill is already loaded. Do NOT call the skill tool for %q.**\n\n", sk.Name)

	if sk.Dir != "" {
		fmt.Fprintf(&sb, "**Skill files: `%s`** — Read input files using absolute paths. Do NOT glob or search.\n", sk.Dir)
		sb.WriteString("**Output files go to the current working directory**, NOT the skill directory.\n\n")
	} else {
		sb.WriteString("(This is a built-in skill with no file-system directory.)\n\n")
	}
	sb.WriteString(sk.Content)

	if len(sk.Files) > 0 {
		sb.WriteString("\n\n## Skill files (absolute paths):\n")
		for _, f := range sk.Files {
			fmt.Fprintf(&sb, "- `%s`\n", f)
		}
	}
	sb.WriteString("</skill_content>")
	return sb.String()
}

func compactSkillMetadata(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
