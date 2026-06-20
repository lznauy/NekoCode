package editdsl

import "strings"

func repairDelimiterBalance(deletedLines, payload []string) []string {
	delOpen, delClose := countDelimiters(deletedLines)
	payOpen, payClose := countDelimiters(payload)

	missingClose := (payOpen - payClose) - (delOpen - delClose)
	if missingClose <= 0 {
		return payload
	}

	closers := extractTrailingClosers(deletedLines)
	if len(closers) == 0 {
		return payload
	}

	appended := 0
	for _, c := range closers {
		if appended >= missingClose {
			break
		}
		payload = append(payload, c)
		appended++
	}
	return payload
}

func countDelimiters(lines []string) (open, close int) {
	inBlockComment := false
	for _, line := range lines {
		o, c, inBlock := countDelimitersInLine(line, inBlockComment)
		open += o
		close += c
		inBlockComment = inBlock
	}
	return
}

func countDelimitersInLine(line string, inBlockComment bool) (open, close int, stillInBlock bool) {
	stillInBlock = inBlockComment
	bs := []byte(line)
	for i := 0; i < len(bs); i++ {
		ch := bs[i]

		if !stillInBlock && i+1 < len(bs) && ch == '/' && bs[i+1] == '/' {
			break
		}
		if !stillInBlock && i+1 < len(bs) && ch == '/' && bs[i+1] == '*' {
			stillInBlock = true
			i++
			continue
		}
		if stillInBlock && i+1 < len(bs) && ch == '*' && bs[i+1] == '/' {
			stillInBlock = false
			i++
			continue
		}
		if stillInBlock {
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			quote := ch
			i++
			for i < len(bs) {
				if bs[i] == '\\' {
					i += 2
					continue
				}
				if bs[i] == quote {
					break
				}
				i++
			}
			continue
		}

		switch ch {
		case '(', '{', '[':
			open++
		case ')', '}', ']':
			close++
		}
	}
	return
}

func extractTrailingClosers(lines []string) []string {
	count := 0
	for i := len(lines) - 1; i >= 0 && isStructuralCloser(lines[i]); i-- {
		count++
	}
	if count == 0 {
		return nil
	}
	closers := make([]string, count)
	base := len(lines) - count
	for i := 0; i < count; i++ {
		closers[i] = lines[base+i]
	}
	return closers
}

func isStructuralCloser(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	hasCloser := false
	for _, ch := range trimmed {
		switch ch {
		case ')', '}', ']':
			hasCloser = true
		case ';', ',':
		default:
			return false
		}
	}
	return hasCloser
}
