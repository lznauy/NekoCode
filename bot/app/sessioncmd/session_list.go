package sessioncmd

import (
	"fmt"
	"strings"

	"nekocode/bot/session"
)

func FormatSessionList(sessions []session.Meta) string {
	if len(sessions) == 0 {
		return "No saved sessions."
	}
	var sb strings.Builder
	sb.WriteString("Saved sessions:\n")
	for _, s := range sessions {
		fmt.Fprintf(&sb, "  %s  %s  %d msgs  %s\n", s.ID, s.Age(), s.MsgCount, s.CWD)
	}
	sb.WriteString("\n/sessions <id> to resume")
	return sb.String()
}

func ResumeFailed(id string, err error) string {
	return fmt.Sprintf("Failed to resume session %s: %v", id, err)
}

func ResumeSuccess(id string, msgCount int) string {
	return fmt.Sprintf("Resumed session %s (%d messages restored).", id, msgCount)
}
