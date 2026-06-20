package shell

import "strings"

func stripQuotedSegments(cmd string) string {
	out := make([]byte, 0, len(cmd))
	i := 0
	for i < len(cmd) {
		ch := cmd[i]
		if ch == '\'' {
			out = append(out, ' ')
			i++
			for i < len(cmd) && cmd[i] != '\'' {
				out = append(out, ' ')
				i++
			}
			if i < len(cmd) {
				out = append(out, ' ')
				i++
			}
		} else if ch == '"' {
			out = append(out, ' ')
			i++
			for i < len(cmd) && cmd[i] != '"' {
				if cmd[i] == '\\' && i+1 < len(cmd) {
					out = append(out, ' ', ' ')
					i += 2
				} else {
					out = append(out, ' ')
					i++
				}
			}
			if i < len(cmd) {
				out = append(out, ' ')
				i++
			}
		} else {
			out = append(out, ch)
			i++
		}
	}
	return string(out)
}

func stripHeredocBodies(cmd string) string {
	clean := stripQuotedSegments(cmd)
	if idx := strings.Index(clean, "<<"); idx >= 0 {
		return cmd[:idx]
	}
	if idx := strings.Index(cmd, "<<"); idx >= 0 && strings.IndexByte(cmd[idx:], '\n') >= 0 {
		return cmd[:idx]
	}
	return cmd
}

func isWriteRedirect(cmd string, idx int, tokLen int) bool {
	rest := strings.TrimSpace(cmd[idx+tokLen:])
	if rest == "" {
		return false
	}
	return !strings.HasPrefix(rest, "/dev/null") && !strings.HasPrefix(rest, "/dev/")
}

func hasWriteRedirection(cmd string) bool {
	clean := stripQuotedSegments(cmd)
	spacedToks := []string{" > ", ">> ", "2> ", " &> ", "1> "}
	for _, tok := range spacedToks {
		pos := 0
		for {
			idx := strings.Index(clean[pos:], tok)
			if idx < 0 {
				break
			}
			idx += pos
			if isWriteRedirect(clean, idx, len(tok)) {
				return true
			}
			pos = idx + 1
		}
	}

	compactToks := []string{">", ">>", "2>", "&>", "1>"}
	for _, tok := range compactToks {
		pos := 0
		for {
			idx := strings.Index(clean[pos:], tok)
			if idx < 0 {
				break
			}
			idx += pos
			next := idx + len(tok)
			if next >= len(clean) || clean[next] == ' ' {
				pos = idx + 1
				continue
			}
			if isWriteRedirect(clean, idx, len(tok)) {
				return true
			}
			pos = idx + 1
		}
	}

	for _, prefix := range []string{"> ", ">> "} {
		if !strings.HasPrefix(clean, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(clean, prefix))
		if rest != "" && !strings.HasPrefix(rest, "/dev/null") && !strings.HasPrefix(rest, "/dev/") {
			return true
		}
	}
	return false
}
