package editdsl

import "fmt"

func resolveBlockHunks(hunks []Hunk, lines []string, resolver BlockResolver, path string) ([]Hunk, error) {
	if resolver == nil {
		for _, h := range hunks {
			if h.Block {
				return nil, fmt.Errorf("block hunk at line %d requires a block resolver (unsupported file type?)", h.Start)
			}
		}
		return hunks, nil
	}

	var result []Hunk
	for _, h := range hunks {
		if !h.Block {
			result = append(result, h)
			continue
		}

		span, err := resolver(path, h.Start)
		if err != nil {
			return nil, fmt.Errorf("block resolution failed at line %d: %w", h.Start, err)
		}
		if span == nil {
			return nil, fmt.Errorf("no code block found at line %d", h.Start)
		}
		if span.Start < 1 || span.End > len(lines) || span.Start > span.End {
			return nil, fmt.Errorf("block at line %d resolved to invalid range %d..%d (file has %d lines)",
				h.Start, span.Start, span.End, len(lines))
		}

		switch h.Kind {
		case HunkReplace:
			result = append(result, Hunk{
				Kind:    HunkReplace,
				Start:   span.Start,
				End:     span.End,
				Payload: h.Payload,
			})
		case HunkDelete:
			result = append(result, Hunk{
				Kind:  HunkDelete,
				Start: span.Start,
				End:   span.End,
			})
		case HunkInsert:
			result = append(result, Hunk{
				Kind:    HunkInsert,
				Start:   span.End,
				Cursor:  CursorAfter,
				Payload: h.Payload,
			})
		}
	}
	return result, nil
}
