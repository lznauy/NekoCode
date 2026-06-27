package edit

func buildChangedLineSet(hunks []editHunk) map[int]bool {
	result := make(map[int]bool)
	for _, h := range hunks {
		if len(h.NewLines) == 0 {
			continue
		}
		for line := h.NewStart; line <= h.NewEnd; line++ {
			if line > 0 {
				result[line] = true
			}
		}
	}
	return result
}
